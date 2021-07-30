// Sample code for building a server in Go
package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/aws/aws-sdk-go/service/dynamodb/expression"
	"github.com/sirupsen/logrus"
)

// Request struct allows for de/serialization of request structs
type Request struct {
	Service string
	Action  string
}

// Server provides shared resources for all calls to the server.
type Server struct {
	Name  string
	ASess *session.Session

	// Params passed in through request
	tableName string
	name      string
	status    string // active or inactive
	active    bool   // true or false
	index []byte
}

// Entry is a direct map of the entity in the dynamoDB table
type Item struct {
	Index   []byte
	Name          string
	Users []string
	Active      bool
}

// DynamoDBList returns all contents of table as json
func (s *Server) DynamoDBList(dynamoDBClient *dynamodb.DynamoDB) (contents []Item, dynamoError error) {
	// Scan dynamodb client by requested table name
	input := &dynamodb.ScanInput{
		TableName: aws.String(s.tableName),
	}
	result, err := dynamoDBClient.Scan(input)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Error("Failed to scan DynamoDB")
		return nil, err
	}

	// Marshal dynamodb contents
	obj := []Item{}
	err = dynamodbattribute.UnmarshalListOfMaps(result.Items, &obj)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Error("Failed to unmarshal DynamoDB record")
		return nil, err
	}
	return obj, nil
}

// DynamoDBQuery returns contents of table by specified name or active status and performs a query returning json output
func (s *Server) DynamoDBQuery(dynamoDBClient *dynamodb.DynamoDB) (contents []Item, dynamoError error) {
	// build condition filter
	var filt expression.ConditionBuilder

	// set active true or false if needed
	if s.status != "" {
		switch {
		case s.status == "active":
			s.active = true
		case s.status == "inactive":
			s.active = false
		}
	}

	// Get all entries that match the filter(s) with all details
	proj := expression.NamesList(expression.Name("index"), expression.Name("name"), expression.Name("users"), expression.Name("active"))
	var expr expression.Expression
	var err error

	// Build the condition filter for name and active status
	if s.name != "" && s.status != "" {
		filt = expression.Name("name").Equal(expression.Value(s.name)).And(expression.Name("active").Equal(expression.Value(s.active)))
	} else {
		// Build the filter for either name or active status
		if s.name != "" && s.status == "" {
			filt = expression.Name("name").Equal(expression.Value(s.name)) // Only query by name
		} else if s.name == "" && s.status != "" {
			filt = expression.Name("active").Equal(expression.Value(s.active)) // Only Query by active status
		}
	}
	// Use expression package to build
	expr, err = expression.NewBuilder().WithFilter(filt).WithProjection(proj).Build()
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Error("Got error building expression")
	}

	// build the query input parameters
	params := &dynamodb.ScanInput{
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		FilterExpression:          expr.Filter(),
		ProjectionExpression:      expr.Projection(),
		TableName:                 aws.String(s.tableName),
	}

	obj := []Item{}
	// make the DynamoDB Scan API call
	result, err := dynamoDBClient.Scan(params)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Error("Query API call failed")
	}
	// unmarshalling
	err = dynamodbattribute.UnmarshalListOfMaps(result.Items, &obj)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Error("Failed to unmarshal query output")
		return nil, err
	}
	return obj, nil
}

// ListHandler reaches out to DynamoDBList and DynamoDBQuery via the /list endpoint
func (s *Server) ListHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/list" {
		http.Error(w, "404 not found.", http.StatusNotFound)
		return
	}
	// we don't support anything except GET to this endpoint
	if r.Method != "GET" {
		http.Error(w, "404 not found, method not supported.", http.StatusNotFound)
		return
	}

	// create dynamoClient and pass in params
	dynamoDBClient := dynamodb.New(s.ASess)

	// reset current server params
	s.tableName = ""
	s.name = ""
	s.status = ""
	s.active = false

	// access required param1, tableName
	param1 := r.URL.Query().Get("tableName")
	s.tableName = param1

	// access optional param2, name
	param2 := r.URL.Query().Get("name")
	if param2 != "" {
		s.name = param2
	}

	// access optional param3, active status
	param3 := r.URL.Query().Get("status")

	// set active status based on active/inactive keywords, no filter for "both" or "all"
	if param3 == "active" || param3 == "inactive" {
		s.status = param3
	}

	// list or query request
	var resp []Item
	if s.name != "" || s.status != "" {
		resp, _ = s.DynamoDBQuery(dynamoDBClient)
	} else { // otherwise just list table contents
		resp, _ = s.DynamoDBList(dynamoDBClient)
	}

	// marshal response and make it pretty
	respJSON, err := json.MarshalIndent(resp, "", "    ")
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Error("Got error marshalling list response")
		return
	}
	// Display the results
	fmt.Fprint(w, string(respJSON))
}

// DyanmoDBGetItem is a helper method that returns the existing item on the table based on index
func (s *Server) DynamoDBGetItem(dynamoDBClient *dynamodb.DynamoDB) (item Item) {
	result, err := dynamoDBClient.GetItem(&dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			"index": {
				B: s.index,
			},
		},
	})
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Error("Failed to call DynamoDBGetItem")
	}
	item = Item{}
	err = dynamodbattribute.UnmarshalMap(result.Item, &item)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Error("Failed to unmarshal GetItem response")
	}
	return item
}

// DynamoDBDeactivate deactivates the entry with the specified name in the table
func (s *Server) DynamoDBDeactivate(dynamoDBClient *dynamodb.DynamoDB) (result Item) {
	log.Println("Entering DynamoDBDeactivate")
	// Build the updateItem input based on active as an attribute value
	input := &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			"index": {
				B: s.index,
			},
		},
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":active": {
				BOOL: aws.Bool(false),
			},
		},
		UpdateExpression: aws.String("set active = :active"), // set active status to false
		ReturnValues:     aws.String("ALL_NEW"),
	}
	var err error
	resp, err := dynamoDBClient.UpdateItem(input)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Error("Failed to call UpdateItem")
		return
	}
	err = dynamodbattribute.UnmarshalMap(resp.Attributes, &result)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Error("Failed to unmarshal in DynamoDBDeactivate")
		return
	}
	return result
}

// DeactivateHandler reaches out to DynamoDBDeactivate via the /deactivate endpoint
func (s *Server) DeactivateHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/deactivate" {
		http.Error(w, "404 not found.", http.StatusNotFound)
		return
	}
	// we don't support anything except GET to this endpoint
	if r.Method != "POST" {
		http.Error(w, "404 not found, method not supported.", http.StatusNotFound)
		return
	}

	// Create dynamoClient and pass in params
	dynamoDBClient := dynamodb.New(s.ASess)

	// Reset current server params
	s.tableName = ""
	s.index = nil

	// Access required param1, tableName
	param1 := r.URL.Query().Get("tableName")
	s.tableName = param1

	// access param2, index
	param2 := r.URL.Query().Get("index")
	log.Println(param2)

	if param2 != "" {
		// Convert the string index that is passed in into a byte
		indexJson, err := json.Marshal(param2)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"error": err.Error(),
			}).Error("Failed to marshal index param")
			return
		}
		var indexByte []byte
		err6 := json.Unmarshal(indexJson, &indexByte)
		if err6 != nil {
			logrus.WithFields(logrus.Fields{
				"error": err.Error(),
			}).Error("Failed to unmarshal index param")
			return
		}

		// Set index for all calls to server
		s.index = indexByte

		// Make a check for whether the entry is already inactive
		currEntry := s.DynamoDBGetItem(dynamoDBClient)
		if !currEntry.Active {
			log.Print("Current entry is already inactive.")
			// Get and format the current response
			currEntryJSON, err := json.MarshalIndent(currEntry, "", "    ")
			if err != nil {
				logrus.WithFields(logrus.Fields{
					"error": err.Error(),
				}).Error("Failed to marshal in DynamoDBGetItem")
				return
			}
			fmt.Fprint(w, "The deactivate request failed: Specified entry is already inactive \n", string(currEntryJSON))

		} else {
			fmt.Print("Current entry is active.")
			// Make the call to deactivate
			resp := s.DynamoDBDeactivate(dynamoDBClient)
			log.Println(resp)

			// Get and format the deactivated entry
			respJSON, err := json.MarshalIndent(resp, "", "    ")
			if err != nil {
				logrus.WithFields(logrus.Fields{
					"error": err.Error(),
				}).Error("Failed to marshal output for the deactivate entry")
				return
			}
			fmt.Fprint(w, "Successfully deactivated the specified entry, setting active status to false: \n", string(respJSON))
		}

	}
}
