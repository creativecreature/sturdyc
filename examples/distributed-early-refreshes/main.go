package main

import (
	"context"
	"log"
	"time"
)

func main() {
	apiClient := newAPIClient(newDistributedStorage())

	for i := 0; i < 10; i++ {
		_, err := apiClient.GetShippingOptions(context.Background(), "1234", "asc")
		if err != nil {
			log.Fatal(err)
		}
		log.Println("The shipping options were retrieved successfully!")
		time.Sleep(250 * time.Millisecond)
	}
}
