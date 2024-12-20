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
				Name:   aws.String("private-dns-name"),
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
	regionAnnotation        = "region"
	runcommandSopAnnotation = "runcommand_sop_name"
)

func GetRunCommandSopName(event types.Event) (string, error) {
	annotations := event.Check.ObjectMeta.GetAnnotations()

	var runcommandname string
	if info, ok := annotations[runcommandSopAnnotation]; !ok {
		return "", fmt.Errorf("Error: runcommand name  not found in the annotation")
	} else {
		runcommandname = info
	}
	return runcommandname, nil
}

func GetRegion(event types.Event) (string, error) {
	var region string
	labels := event.Entity.ObjectMeta.GetLabels()

	if info, ok := labels[regionAnnotation]; !ok {
		return "", fmt.Errorf("Error: Region not found in the map")
	} else {
		region = info
	}
	return region, nil
}

// Handler function for parsing annotations and firing SSM command
func handler(event types.Event) error {
	// Extract annotations

	runcommandname, err := GetRunCommandSopName(event)
	if err != nil {
		return err
	}

	region, err := GetRegion(event)
	if err != nil {
		return err
	}

	ipAddress := event.Entity.System.Hostname
	if ipAddress != "" {
		log.Println(ipAddress)
	}

	// Initialize AWS session
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	},
	)
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
	//command := fmt.Sprintf("export container_id_nginx=$(sudo docker ps | grep %s | awk  {'print $1'}) && echo ${container_id_nginx} && sudo docker stop ${container_id_nginx}", containerName)
	input := &ssm.SendCommandInput{
		DocumentName: aws.String(runcommandname),
		InstanceIds:  []*string{aws.String(instanceID)},
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
