package main

import (
	"log"
	"time"

	"github.com/eddielth/iec104"
)

func main() {

	client := iec104.NewClient("127.0.0.1:2404", 5*time.Second)
	client.EnableLog()

	if err := client.Connect(); err != nil {
		log.Fatalf("unable to connect server: %v", err)
	}

	defer client.Close()

	if err := client.GeneralInterrogation(0x0001); err != nil {
		log.Fatalf("unable to execute general interrogation: %v", err)
	}
	log.Printf("general interrogation executed successfully")

	value, quality, err := client.ReadMeasuredValue(1, 1001)
	if err != nil {
		log.Fatalf("unable to read measured value: %v", err)
	}
	log.Printf("measured value: %.2f, quality: 0x%02X", value, quality)

	// status, err := client.ReadSinglePoint(0x0001, 0x000200)
	// if err != nil {
	// 	logger.Printf("unable to read single point: %v", err)
	// } else {
	// 	logger.Printf("single point: %v", status)
	// }

	time.Sleep(1 * time.Second)

}
