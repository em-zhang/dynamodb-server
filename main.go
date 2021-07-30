package main

import (
    "fmt"
    "log" 
    "net/http"
    "os"

    "github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/credentials"
    "github.com/aws/aws-sdk-go/aws/session"
    "github.com/sirupsen/logrus"
    server "github.com/em-zhang/dynamodb-server"
)

func main() {
    // Instantiate server object
    srv := server.Server{} // referencing server.go file

    enviro := os.Getenv("ENVIRONMENT")
    if enviro == "" {
        enviro = "dev" // Default to dev environment
    }

    // == AWS Session ==
    // talking to local dynamoDB container, need it for actual AWS instance as well
    var aerr error
    srv.ASess, aerr = session.NewSession()
    if enviro == "dev" {
        srv.ASess, aerr = session.NewSession(&aws.Config{
            Region: aws.String("us-west-2"),
            Endpoint:    aws.String("http://localhost:8000"),
            Credentials: credentials.NewStaticCredentials("empty", "empty", ""),
        })
    }

    if aerr != nil {
        logrus.WithFields(logrus.Fields{
            "error": aerr.Error(),
        }).Fatal("unable to create AWS session")
    }

    http.HandleFunc("/list", srv.ListHandler)
    http.HandleFunc("/deactivate", srv.DeactivateHandler)

    fmt.Printf("\nStarting server at port 8000\n")
    http.ListenAndServe(":8000", nil)

    if err := http.ListenAndServe(":8080", nil); err != nil {
        log.Fatal(err)
    }
}