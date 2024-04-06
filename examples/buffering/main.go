package main

import (
	"context"
	"log"
	"time"
)

type OrderOptions struct {
	CarrierName        string
	LatestDeliveryTime string
}

func main() {
	// We will fetch these IDs using various option sets, meaning that the ID alone
	// won't be enough to uniquely identify a record. Instead, the cache is going to
	// store each record by combining the ID with the permutations of the options.
	ids := []string{"id1", "id2", "id3", "id4", "id5"}
	optionSetOne := OrderOptions{CarrierName: "FEDEX", LatestDeliveryTime: "2024-04-06"}
	optionSetTwo := OrderOptions{CarrierName: "DHL", LatestDeliveryTime: "2024-04-07"}
	optionSetThree := OrderOptions{CarrierName: "UPS", LatestDeliveryTime: "2024-04-08"}

	// We'll create an API client that uses the cache internally.
	orderClient := NewOrderAPI()
	ctx := context.Background()

	// Next, we'll fetch the entire list of IDs for all options sets.
	log.Println("Filling the cache with all IDs for all option sets")
	orderClient.OrderStatus(ctx, ids, optionSetOne)
	orderClient.OrderStatus(ctx, ids, optionSetTwo)
	orderClient.OrderStatus(ctx, ids, optionSetThree)
	log.Println("Cache filled")

	// At this point, the cache has stored each record individually for each option set:
	// FEDEX-2024-04-06-id1
	// DHL-2024-04-07-id1
	// UPS-2024-04-08-id1
	// etc..

	// The orderClient has configured the cache to start refreshing the records
	// for each set of options in batches of 5, doing so at random intervals
	// between 1 and 2 seconds. This ensures that the refreshes are distributed
	// evenly over time. If a batch is not filled within 30 seconds, the cache
	// will refresh the records regardless. To illustrate, let's sleep for 2
	// seconds to ensure every record is due for a refresh, and then start
	// fetching the ids one by one for every option set:
	time.Sleep(2 * time.Second)
	options := []OrderOptions{optionSetOne, optionSetTwo, optionSetThree}
	for i := 0; i < 1000; i++ {
		// I'm not going to log the response here because it would flood the
		// output, but the cache is going to return the record immediately while it
		// buffers enough ids to perform a refresh for each unique set of options.
		_, err := orderClient.OrderStatus(ctx, []string{ids[i%5]}, options[i%3])
		if err != nil {
			panic(err)
		}
	}

	// Even though we called our API 1000 times, we can see from the logs that it
	// only resulted in 3 outgoing requests:
	// IDs: [id3 id4 id1 id2 id5], carrier: UPS, latest delivery time: 2024-04-08
	// IDs: [id5 id2 id1 id4 id3], carrier: DHL, latest delivery time: 2024-04-07
	// IDs: [id1 id4 id2 id5 id3], carrier: FEDEX, latest delivery time: 2024-04-06
}
