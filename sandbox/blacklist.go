package sandbox

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

const BLACKLIST_FILENAME = "blacklist.txt"

func (sb *sandboxStruct) LoadBlacklist() {
	start := time.Now()
	file, err := os.OpenFile(BLACKLIST_FILENAME, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		sb.ids = append(sb.ids, strings.TrimSpace(scanner.Text()))
	}

	fmt.Printf("[TXT] Loaded %d hashes in %v\n", len(sb.ids), time.Since(start))
}

func (sb *sandboxStruct) SaveBlacklist() {
	file, err := os.OpenFile(BLACKLIST_FILENAME, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)

	for _, hash := range sb.ids {
		_, err := writer.WriteString(hash + "\n")
		if err != nil {
			log.Fatal(err)
		}
	}

	err = writer.Flush()
	if err != nil {
		log.Fatal(err)
	}
}
