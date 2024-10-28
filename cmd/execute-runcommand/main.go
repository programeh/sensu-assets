package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ssm"
	"gopkg.in/matryer/try.v1"
)

var instance_id []*string
var ssmOutput *ssm.Command

func fetchMetaData(value string) string {
	client := &http.Client{}

	req, err := http.NewRequest("PUT", "http://169.254.169.254/latest/api/token", nil)
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "3600")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Error making PUT request: %v", err)
	}
	defer resp.Body.Close()

	token, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading token response body: %v", err)
	}

	url := "http://169.254.169.254/latest/meta-data/" + value + "/"
	req, err = http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Error creating GET request: %v", err)
	}
	// Pass the token in the header
	req.Header.Set("X-aws-ec2-metadata-token", strings.TrimSpace(string(token)))

	// Step 3: Execute the request
	resp, err = client.Do(req)
	if err != nil {
		log.Fatalf("Error making GET request: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error while reading metadata response body: %v", err)
	}
	return string(body)
}

var (
	myRetryer = client.DefaultRetryer{
		NumMaxRetries:    10,
		MaxThrottleDelay: 2 * time.Second,
	}
	sess = session.Must(session.NewSessionWithOptions(
		session.Options{
			Config: aws.Config{
				Retryer: myRetryer,
				Region:  aws.String(fetchMetaData("placement/region")),
			},
			SharedConfigState: session.SharedConfigEnable,
		}))
	ssmSession = ssm.New(sess)
	command    = "export container_id_nginx=$(sudo docker ps | grep nginx | awk  {'print $1'}) && echo ${container_id_nginx} && sudo docker stop ${container_id_nginx}"
)

func main() {

	input := ssm.SendCommandInput{
		TimeoutSeconds: aws.Int64(300),
		InstanceIds:    instance_id,
		DocumentName:   aws.String("AWS-RunShellScript"),
		Comment:        aws.String("Check Disk Errors in eventstore volume triggered by Sensu"),
	}
	input.Parameters = map[string][]*string{
		"commands":         aws.StringSlice([]string{command}),
		"executionTimeout": aws.StringSlice([]string{fmt.Sprintf("%d", 3600)}),
	}

	var output *ssm.SendCommandOutput
	rand.Seed(time.Now().UnixNano())
	time.Sleep(time.Duration(rand.Intn(3600)) * time.Second)
	err := try.Do(func(attempt int) (bool, error) {
		var err error
		output, err = ssmSession.SendCommand(&input)
		ssmOutput = output.Command
		if err != nil {
			time.Sleep(time.Duration(rand.Intn(7200)) * time.Second)
		}
		return attempt < 2, err
	})
	if err != nil {
		fmt.Printf("Error Invoking SSM Run Command: %s", err)
		os.Exit(3)
	}

	var wg sync.WaitGroup
	wg.Wait()
	err = ssmSession.WaitUntilCommandExecuted(&ssm.GetCommandInvocationInput{
		CommandId:  output.Command.CommandId,
		InstanceId: instance_id[0],
	})

	if err != nil {
		fmt.Printf("Error Executing command on Target Instance %s. Failed with Error %s", *instance_id[0], err)
		os.Exit(2)
	}

	if ssmOutput == nil {
		fmt.Println("Command not yet Executed")
		os.Exit(1)
	} else {
		if (*(ssmOutput).Status) == "Pending" {
			resp, err := ssmSession.GetCommandInvocation(&ssm.GetCommandInvocationInput{
				CommandId:  output.Command.CommandId,
				InstanceId: instance_id[0],
			})
			if err != nil {
				fmt.Printf("Error Fetching Command Output after Executing %s", err)
				os.Exit(2)
			} else {
				diskCheckOutput := aws.StringValue(resp.StandardOutputContent)
			
					fmt.Println(diskCheckOutput)
					os.Exit(0)
			}
		}
	}
}

func init() {

	client := &http.Client{}
	req, err := http.NewRequest("PUT", "http://169.254.169.254/latest/api/token", nil)
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "3600")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Error making PUT request: %v", err)
	}
	defer resp.Body.Close()

	token, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading token response body: %v", err)
	}

	url := "http://169.254.169.254/latest/meta-data/instance-id/"
	req, err = http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Error creating GET request for instanceID: %v", err)
	}
	// Pass the token in the header
	req.Header.Set("X-aws-ec2-metadata-token", strings.TrimSpace(string(token)))

	// Step 3: Execute the request
	resp, err = client.Do(req)
	if err != nil {
		log.Fatalf("Error making GET request while getting instanceID: %v", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error while reading metadata response body: %v", err)
	}
	id := string(body[:])
	instance_id = append(instance_id, &id)
}