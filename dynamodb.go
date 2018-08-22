package main

import (
	"github.com/urfave/cli"
	"sync"
	"fmt"
	"strconv"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"time"
	"os"
	"encoding/json"
	"io/ioutil"
)

var dynamodbCommand = func() cli.Command {
	return cli.Command{
		Name: "dynamodb",
		Subcommands: []cli.Command{
			{
				Name:  "purge",
				Usage: "fast purge dynamodb using scan operation",
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "profile",
						Value: "",
					},
					cli.StringFlag{
						Name:  "table-name",
						Value: "",
					},
					cli.StringSliceFlag{
						Name: "key",
					},
					cli.StringFlag{
						Name:  "max-concurrent-pages-delete",
						Value: "100",
						Usage: "Max number of concurrent delete pages returned by scan operation",
					},
				},
				Action: func(c *cli.Context) error {
					profile := c.String("profile")
					tableName := c.String("table-name")
					maxConcurrent, err := strconv.Atoi(c.String("max-concurrent-pages-delete"))
					if err != nil {
						return err
					}
					sess := NewSessionWithSharedProfile(profile)

					ddbc := dynamodb.New(sess, &aws.Config{
						Region: aws.String(endpoints.EuWest1RegionID),
					})


					purge := func(items []map[string]*dynamodb.AttributeValue, pageNr int, wg *sync.WaitGroup) {
						defer wg.Done()

						for _, element := range items {
							key := map[string]*dynamodb.AttributeValue{}
							keys := c.StringSlice("key")
							for _, myKey := range keys {
								key[myKey] = element[myKey]
							}

							time.Sleep(time.Duration(10) * time.Millisecond)

							deleteItemParam := dynamodb.DeleteItemInput{
								TableName: &tableName,
								Key:       key,
							}
							retry := 1
							for {
								_, err := ddbc.DeleteItem(&deleteItemParam)
								if err != nil {
									if aerr, ok := err.(awserr.Error); ok {
										switch aerr.Code() {
										case dynamodb.ErrCodeConditionalCheckFailedException:
											fmt.Println(dynamodb.ErrCodeConditionalCheckFailedException, aerr.Error())
										case dynamodb.ErrCodeProvisionedThroughputExceededException:
											fmt.Println(dynamodb.ErrCodeProvisionedThroughputExceededException, aerr.Error())
										case dynamodb.ErrCodeResourceNotFoundException:
											fmt.Println(dynamodb.ErrCodeResourceNotFoundException, aerr.Error())
										case dynamodb.ErrCodeItemCollectionSizeLimitExceededException:
											fmt.Println(dynamodb.ErrCodeItemCollectionSizeLimitExceededException, aerr.Error())
										case dynamodb.ErrCodeInternalServerError:
											fmt.Println(dynamodb.ErrCodeInternalServerError, aerr.Error())
										default:
											fmt.Println(aerr.Error())
										}
									} else {
										fmt.Println(err.Error())
									}
									sleepTime := retry * retry * 100
									time.Sleep(time.Duration(sleepTime) * time.Millisecond)
									retry = retry + 1
									fmt.Printf("Sleeping for %d milliseconds. Retry DeleteItem... \n", sleepTime)
								} else {
									break
								}
							}
						}
						fmt.Printf("deleted: %d\n", len(items))
					}

					params := dynamodb.ScanInput{
						TableName: &tableName,
					}
					pageNr := 0
					var wg sync.WaitGroup
					err = ddbc.ScanPages(&params, func(output *dynamodb.ScanOutput, b bool) bool {
						targetMap := make([]map[string]*dynamodb.AttributeValue, len(output.Items))
						for key, value := range output.Items {
							targetMap[key] = value
						}
						wg.Add(1)
						go purge(output.Items, pageNr, &wg)
						pageNr += 1

						if pageNr % maxConcurrent == 0 {
							wg.Wait()
						}
						return b == false
					})
					if err != nil {
						return err
					}

					wg.Wait()

					return nil
				},
			},

			{
				Name:  "capacity",
				Usage: "update read and write capacity",
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "profile",
						Value: "",
					},
					cli.StringFlag{
						Name:  "table-name",
						Value: "",
					},
					cli.StringFlag{
						Name:  "read",
						Value: "10",
					},
					cli.StringFlag{
						Name:  "write",
						Value: "10",
					},
					cli.StringSliceFlag{
						Name: "global-secondary-index",
					},
				},
				Action: func(c *cli.Context) error {
					profile := c.String("profile")
					tableName := c.String("table-name")
					gsis := c.StringSlice("global-secondary-index")

					read, err := strconv.ParseInt(c.String("read"), 10, 0)
					if err != nil {
						return err
					}

					write, err := strconv.ParseInt(c.String("write"), 10, 0)
					if err != nil {
						return err
					}

					sess := NewSessionWithSharedProfile(profile)

					ddbc := dynamodb.New(sess, &aws.Config{
						Region: aws.String(endpoints.EuWest1RegionID),
					})

					var input *dynamodb.UpdateTableInput

					if len(gsis) > 0 {
						var gsiu []*dynamodb.GlobalSecondaryIndexUpdate
						for _, indexName := range gsis {
							gsiu = append(gsiu, &dynamodb.GlobalSecondaryIndexUpdate{
								Update: &dynamodb.UpdateGlobalSecondaryIndexAction{
									IndexName: &indexName,
									ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
										ReadCapacityUnits:  aws.Int64(read),
										WriteCapacityUnits: aws.Int64(write),
									},
								},
							})
						}

						input = &dynamodb.UpdateTableInput{
							ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
								ReadCapacityUnits:  aws.Int64(read),
								WriteCapacityUnits: aws.Int64(write),
							},
							GlobalSecondaryIndexUpdates: gsiu,
							TableName:                   aws.String(tableName),
						}
					} else {
						input = &dynamodb.UpdateTableInput{
							ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
								ReadCapacityUnits:  aws.Int64(read),
								WriteCapacityUnits: aws.Int64(write),
							},
							TableName: aws.String(tableName),
						}
					}

					update, err := ddbc.UpdateTable(input)
					fmt.Printf("updated %v", update)

					if err != nil {
						return err
					}

					return nil
				},
			},
			{
				Name:  "describe-table",
				Usage: "get table details",
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "profile",
						Value: "",
					},
					cli.StringFlag{
						Name:  "table-name",
						Value: "",
					},
				},
				Action: func(c *cli.Context) error {
					profile := c.String("profile")
					tableName := c.String("table-name")

					sess := NewSessionWithSharedProfile(profile)

					ddbc := dynamodb.New(sess, &aws.Config{
						Region: aws.String(endpoints.EuWest1RegionID),
					})

					input := dynamodb.DescribeTableInput{
						TableName: &tableName,
					}

					output, err := ddbc.DescribeTable(&input)
					fmt.Printf("updated %v", output)

					if err != nil {
						return err
					}

					return nil
				},
			},
			{
				Name:  "count-item",
				Usage: "counts elements in table using scan operation",
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "profile",
						Value: "",
					},
					cli.StringFlag{
						Name:  "table-name",
						Value: "",
					},
				},
				Action: func(c *cli.Context) error {
					profile := c.String("profile")
					tableName := c.String("table-name")

					sess := NewSessionWithSharedProfile(profile)

					ddbc := dynamodb.New(sess, &aws.Config{
						Region: aws.String(endpoints.EuWest1RegionID),
					})

					params := dynamodb.ScanInput{
						TableName: &tableName,
					}
					nrOfItems := 0
					err := ddbc.ScanPages(&params, func(output *dynamodb.ScanOutput, b bool) bool {
						nrOfItems += len(output.Items)
						return b == false
					})

					fmt.Printf("nr of items: %v", nrOfItems)

					if err != nil {
						return err
					}

					return nil
				},
			},
			{
				Name:  "put-item",
				Usage: "put item defined in input.json",
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "profile",
						Value: "",
					},
					cli.StringFlag{
						Name:  "table-name",
						Value: "",
					},
				},
				Action: func(c *cli.Context) error {
					profile := c.String("profile")
					tableName := c.String("table-name")
					var item map[string]*dynamodb.AttributeValue

					jsonFile, err := os.Open("input.json")
					if err != nil {
						fmt.Println("Error when opening input.json")
						return err
					}
					defer jsonFile.Close()

					byteValue, _ := ioutil.ReadAll(jsonFile)
					err = json.Unmarshal(byteValue, &item)
					if err != nil {
						return err
					}

					sess := NewSessionWithSharedProfile(profile)

					ddbc := dynamodb.New(sess, &aws.Config{
						Region: aws.String(endpoints.EuWest1RegionID),
					})

					input := dynamodb.PutItemInput{
						Item:                   item,
						TableName:              &tableName,
						ReturnConsumedCapacity: aws.String("TOTAL"),
					}

					result, err := ddbc.PutItem(&input)
					if err != nil {
						if aerr, ok := err.(awserr.Error); ok {
							return aerr
						} else {
							return err
						}
					}

					fmt.Printf("result: %v", result)

					return nil
				},
			},
			{
				Name:  "restore-table-to-point-in-time",
				Usage: "restore table to point in time",
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "profile",
						Value: "",
					},
					cli.StringFlag{
						Name:  "source-table-name",
						Value: "",
					},
					cli.StringFlag{
						Name:  "target-table-name",
						Value: "",
					},
				},
				Action: func(c *cli.Context) error {
					profile := c.String("profile")
					sourceTableName := c.String("source-table-name")
					targetTableName := c.String("target-table-name")

					sess := NewSessionWithSharedProfile(profile)

					ddbc := dynamodb.New(sess, &aws.Config{
						Region: aws.String(endpoints.EuWest1RegionID),
					})

					input := dynamodb.RestoreTableToPointInTimeInput{
						SourceTableName: &sourceTableName,
						TargetTableName: &targetTableName,
						// TODO [grokrz]: parameter ?
						UseLatestRestorableTime: aws.Bool(true),
					}

					result, err := ddbc.RestoreTableToPointInTime(&input)
					if err != nil {
						if aerr, ok := err.(awserr.Error); ok {
							return aerr
						} else {
							return err
						}
					}

					fmt.Printf("result: %v", result)

					return nil
				},
			},
		},
	}
}
