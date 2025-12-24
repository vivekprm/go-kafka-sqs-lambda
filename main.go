package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
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

type ResourceStatus struct {
	InstanceID string `json:"instance-id"`
	State      string `json:"state"`
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
	return "Event{DetailType: " + e.DetailType + ", InstanceID: " + e.Detail.InstanceID + ", Status: " + e.Detail.State + "}"
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

	ec2InstanceID := ev.Detail.InstanceID
	// Needs describe instances permission
	out, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
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
