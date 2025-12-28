package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

/*
*
Test event for lambda:

	{
	  "detail-type": "instance status changed",
	  "detail": {
	    "instance_id": "i-0c8c2400b4b05dace",
	    "status": "stopping"
	  }
	}
*/
var (
	ec2Client *ec2.Client
	sqsClient *sqs.Client
)

/**
{
    "version": "0",
    "id": "f80873c9-3942-6889-956d-51b8df604e3d",
    "detail-type": "AWS API Call via CloudTrail",
    "source": "aws.ec2",
    "account": "643716337869",
    "time": "2025-12-27T16:11:35Z",
    "region": "ap-south-1",
    "resources": [],
    "detail": {
        "eventVersion": "1.11",
        "userIdentity": {
            "type": "Root",
            "principalId": "643716337869",
            "arn": "arn:aws:iam::643716337869:root",
            "accountId": "643716337869",
            "accessKeyId": "ASIAZLYD32DGWXISP2R6",
            "sessionContext": {
                "attributes": {
                    "creationDate": "2025-12-27T13:04:19Z",
                    "mfaAuthenticated": "true"
                }
            }
        },
        "eventTime": "2025-12-27T16:11:31Z",
        "eventSource": "ec2.amazonaws.com",
        "eventName": "StartInstances",
        "awsRegion": "ap-south-1",
        "sourceIPAddress": "223.178.208.32",
        "userAgent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
        "requestParameters": {
            "instancesSet": {
                "items": [
                    {
                        "instanceId": "i-0c8c2400b4b05dace"
                    }
                ]
            }
        },
        "responseElements": {
            "requestId": "d54bb834-6ef3-4b09-bc4e-7bd8fe3f0ef7",
            "instancesSet": {
                "items": [
                    {
                        "instanceId": "i-0c8c2400b4b05dace",
                        "currentState": {
                            "code": 0,
                            "name": "pending"
                        },
                        "previousState": {
                            "code": 80,
                            "name": "stopped"
                        }
                    }
                ]
            }
        },
        "requestID": "d54bb834-6ef3-4b09-bc4e-7bd8fe3f0ef7",
        "eventID": "01dd28f1-7cf3-46bd-bd45-c99d5839a376",
        "readOnly": false,
        "eventType": "AwsApiCall",
        "managementEvent": true,
        "recipientAccountId": "643716337869",
        "eventCategory": "Management",
        "tlsDetails": {
            "tlsVersion": "TLSv1.3",
            "cipherSuite": "TLS_AES_128_GCM_SHA256",
            "clientProvidedHostHeader": "ec2.ap-south-1.amazonaws.com"
        },
        "sessionCredentialFromConsole": "true"
    }
}
*/

type InstancesSet struct {
	Items	   []Instance   `json:"items"`
}

type State struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

type Instance struct {
	InstanceID    string `json:"instanceId"`
	CurrentState  State  `json:"currentState"`
	PreviousState State  `json:"previousState"`
}
type ResponseElements struct {
	InstancesSet InstancesSet `json:"instancesSet"`
	RequestID	string       `json:"requestId"`
}
type ResourceStatus struct {
	ResponseElements	 ResponseElements `json:"responseElements"`
}

type Event struct {
	DetailType string            `json:"detail-type"`
	Detail     ResourceStatus    `json:"detail"`
	Tags       map[string]string `json:"tags,omitempty"`
	Source     string            `json:"source,omitempty"`
	Account    string            `json:"account,omitempty"`
	Region     string            `json:"region,omitempty"`
	Time       string            `json:"time,omitempty"`
}

func (e Event) String() string {
	return "Event{DetailType: " + e.DetailType + ", InstanceID: " + e.Detail.ResponseElements.InstancesSet.Items[0].InstanceID + ", Status: " + e.Detail.ResponseElements.InstancesSet.Items[0].CurrentState.Name + "}"
}

func init() {
	// Initialize the S3 client outside of the handler, during the init phase
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	ec2Client = ec2.NewFromConfig(cfg)
	sqsClient = sqs.NewFromConfig(cfg)
}

func handleRequest(ctx context.Context, event json.RawMessage) error {
	log.Println("Received event:", string(event))
	var ev Event
	err := json.Unmarshal([]byte(event), &ev)
	if err != nil {
		log.Fatalf("Error unmarshaling event: %v", err)
	}
	log.Println(ev.String())

	srcAccount := ev.Account
	region := ev.Region
	ec2InstanceID := ev.Detail.ResponseElements.InstancesSet.Items[0].InstanceID

	roleArn := fmt.Sprintf(
		"arn:aws:iam::%s:role/central-ec2-tag-reader",
		srcAccount,
	)

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("Error loading default config: %v", err)
	}

	stsSvc := sts.NewFromConfig(cfg)
	
	log.Printf("Attempting AssumeRole into source account %s", srcAccount)

	assumed, err := stsSvc.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn:         aws.String(roleArn),
		RoleSessionName: aws.String("ec2-tag-enrichment"),
	})

	if err != nil {
		log.Fatalf("Failed to assume role %s: %v", roleArn, err)
	}

	log.Printf("AssumeRole succeeded for %s", roleArn)

	creds := aws.NewCredentialsCache(
		credentials.NewStaticCredentialsProvider(
			*assumed.Credentials.AccessKeyId,
			*assumed.Credentials.SecretAccessKey,
			*assumed.Credentials.SessionToken,
		),
	)

	ec2Cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(creds),
	)

	ec2Svc := ec2.NewFromConfig(ec2Cfg)

	out, err := ec2Svc.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{ec2InstanceID},
	})

	if err != nil {
		log.Fatalf("Failed to describe instance %s: %v", ec2InstanceID, err)
	}

	tags := out.Reservations[0].Instances[0].Tags

	evTags := map[string]string{}
	for _, tag := range tags {
		evTags[*tag.Key] = *tag.Value
	}

	ev.Tags = evTags

	enrichedEvent, err := json.Marshal(ev)
	if err != nil {
		log.Fatalf("Error marshaling event: %v", err)
	}

	queueURL := os.Getenv("SQS_QUEUE_URL")
	// Needs write access to the SQS queue
	sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(enrichedEvent)),
	})

	log.Printf("Sent enriched event with tags to SQS for site: %s", evTags["f5xc-site-name"])

	return nil
}

func main() {
	lambda.Start(handleRequest)
}
