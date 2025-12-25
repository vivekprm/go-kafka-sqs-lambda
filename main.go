package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

/**
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

	topic := os.Getenv("KAFKA_TOPIC")
	bootstrapServers := os.Getenv("KAFKA_BOOTSTRAP_SERVERS")

	log.Printf("Producing to Kafka topic %s at %s\n", topic, bootstrapServers)

	p, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers": bootstrapServers,
		"client.id":         "aws_lambda",
		"acks":              "all",
	})

	if err != nil {
		log.Fatalf("Failed to create producer: %s\n", err)
	}
	delivery_chan := make(chan kafka.Event, 10000)
	err = p.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          []byte(enrichedEvent)},
		delivery_chan,
	)

	if err != nil {
		log.Fatalf("Failed to produce message: %s\n", err)
	}
	e := <-delivery_chan
	m := e.(*kafka.Message)

	if m.TopicPartition.Error != nil {
		log.Printf("Delivery failed: %v\n", m.TopicPartition.Error)
	} else {
		log.Printf("Delivered message to topic %s [%d] at offset %v\n",
			*m.TopicPartition.Topic, m.TopicPartition.Partition, m.TopicPartition.Offset)
	}
	close(delivery_chan)
	return nil
}

func main() {
	lambda.Start(handleRequest)
}
