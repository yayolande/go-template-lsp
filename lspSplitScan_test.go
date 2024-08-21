package main

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestSplitScanLSP(t *testing.T) {
	tests := []struct {
		input string
		want string
	}{
		{
			input: "Content-Length: 12\r\n" + "\r\n" + "{'num': 597}",
			want: "{'num': 597}",
		},
		{
			input: "Content-Length: 19\r\n" + "Content-Type: utf8\r\n" + "\r\n" + "{'user': 'steveen'}",
			want: "{'user': 'steveen'}",
		},
	}

	for count, test := range tests {
		testName := strconv.Itoa(count) + "_" + test.input[:8] + "_test"

		t.Run(testName, func(t *testing.T) {
			str := test.input
			scanner := bufio.NewScanner(strings.NewReader(str))
			scanner.Split(generateScanLspSplitter())

			answer := ""
			count := 1	// count should never go above 'count + 1'

			for scanner.Scan() {
				answer = scanner.Text()

				fmt.Println("Text: ", answer)
				count ++
			}

			if scanner.Err() != nil {
				t.Error("Error while scanning reader: ", scanner.Err())
			}

			// TODO: Need to handle the worst case
			if answer != test.want {
				t.Errorf("Error -- count : %d\n Expected : %s \n Got : %s", count, test.want, answer)
			}
		})
	}
}
