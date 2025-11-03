package main

import (
	"log"
	"time"

	"github.com/eddielth/iec104"
)

func main() {

	client := iec104.NewClient("127.0.0.1:2404", 5*time.Second)
	// client.EnableLog()

	if err := client.Connect(); err != nil {
		log.Fatalf("unable to connect server: %v", err)
	}

	defer client.Close()

	for {
		result, err := client.GeneralInterrogation(0x0001)
		if err != nil {
			log.Fatalf("unable to execute general interrogation: %v", err)
		}
		log.Printf("general interrogation result: %v", result)

		time.Sleep(60 * time.Second)
	}

	// for {

	// 	value, quality, err := client.ReadMeasuredValue(1, 16386)
	// 	if err != nil {
	// 		log.Printf("unable to read measured value: %v", err)
	// 	} else {
	// 		log.Printf("measured value: %v, quality: 0x%02X", value, quality)
	// 	}
	// 	time.Sleep(1 * time.Second)
	// }

}
