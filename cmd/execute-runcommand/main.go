package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/sensu/sensu-go/types"
)

func getInstanceIDByIP(ec2Client *ec2.EC2, ipAddress string) (string, error) {
	input := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("private-ip-address"),
				Values: []*string{aws.String(ipAddress)},
			},
		},
	}

	result, err := ec2Client.DescribeInstances(input)
	if err != nil {
		return "", fmt.Errorf("failed to describe instances: %w", err)
	}

	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			return *instance.InstanceId, nil
		}
	}
	return "", fmt.Errorf("instance with IP address %s not found", ipAddress)
}

// SensuCheckAnnotation keys
const (
	regionAnnotation = "region"
	containerName    = "testrepo" // Name of the container to restart
)

// Handler function for parsing annotations and firing SSM command
func handler(event types.Event) error {
	// Extract annotations
	var region string
	labels := event.Entity.ObjectMeta.GetLabels()
	if info, ok := labels[regionAnnotation]; !ok {
		region = info
		return fmt.Errorf("Error: Region not found in the map")
	}
	ipAddress := event.Entity.System.Hostname

	// Initialize AWS session
	sess, err := session.NewSession(&aws.Config{Region: aws.String(region)})
	if err != nil {
		return fmt.Errorf("failed to create AWS session: %w", err)
	}

	ec2Client := ec2.New(sess)
	instanceID, err := getInstanceIDByIP(ec2Client, ipAddress)
	if err != nil {
		return fmt.Errorf("failed to get instance ID for IP %s: %w", ipAddress, err)
	}

	ssmClient := ssm.New(sess)

	// Define SSM command to restart container
	command := fmt.Sprintf("export container_id_nginx=$(sudo docker ps | grep %s | awk  {'print $1'}) && echo ${container_id_nginx} && sudo docker stop ${container_id_nginx}", containerName)
	input := &ssm.SendCommandInput{
		DocumentName: aws.String("AWS-RunShellScript"),
		Parameters: map[string][]*string{
			"commands": {aws.String(command)},
		},
		InstanceIds: []*string{aws.String(instanceID)},
	}

	// Execute SSM command
	result, err := ssmClient.SendCommand(input)
	if err != nil {
		return fmt.Errorf("failed to execute SSM command: %w", err)
	}

	log.Printf("SSM command sent successfully: %s", *result.Command.CommandId)
	return nil
}

func main() {
	// Read Sensu event data from stdin
	var event types.Event
	log.Printf("start Remediation")
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		log.Fatalf("Error decoding Sensu event: %v", err)
	}

	if err := handler(event); err != nil {
		log.Fatalf("Handler error: %v", err)
	}

	log.Printf("End Remediation")
}
