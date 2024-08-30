package main

import (
	"bufio"
	"strconv"
	"strings"
	"testing"
)

func TestSplitScanLSP(t *testing.T) {
	tests := []struct {
		input string
		wants []string
		isError bool
	}{
		{
			input: "Content-Length: 12\r\n" + "\r\n" + "{'num': 597}",
			wants: []string{"{'num': 597}"},
		},
		{
			input: "Content-Length: 10\r\n" + "\r\n" + "{'num': 597}",
			wants: []string{"{'num': 59"},
		},
		{
			input: "Content-Type: utf8\r\n" + "\r\n" + "{'num': 597}",
			wants: []string{""},
		},
		{
			input: "Content-Type: utf8\r\n" + "\r\n" + "{'Content-Length': 22}",
			wants: []string{""},
		},
		{
			input: "Content-Length: 22\r\n" + "Content-Type: utf8\r\n" + "\r\n" + "{'Content-Length': 27}",
			wants: []string{"{'Content-Length': 27}"},
		},
		{
			input: "Content-Type: utf8\r\n" + "Content-Length: 22\r\n" + "\r\n" + "{'Content-Length': 27}",
			wants: []string{"{'Content-Length': 27}"},
		},
		{
			input: "Content-Length 22\r\n" + "Content-Type: utf8\r\n" + "\r\n" + "{'Content-Length': 27}",
			wants: []string{""},
		},
		{
			input: "Content-Length: 19\r\n" + "Content-Type: utf8\r\n" + "\r\n" + "{'user': 'steveen'}",
			wants: []string{"{'user': 'steveen'}"},
		},
		{
			input: "Content-Length: 19.2\r\n" + "Content-Type: utf8\r\n" + "\r\n" + "{'user': 'steveen'}",
			wants: []string{""},
		},
		{
			input: "Content-Length: 19,2\r\n" + "Content-Type: utf8\r\n" + "\r\n" + "{'user': 'steveen'}",
			wants: []string{""},
		},
		{
			input: "Content-Length: 19\r\n" + "Content-Type: utf8\r\n" + "Authorization: SIMPLE\r\n" + "\r\n" + "{'user': 'steveen'}",
			wants: []string{"{'user': 'steveen'}"},
		},
		{
			input: "Content-Length: 19\r\n" + "Content-Type: utf8\r\n" + "Authorization: SIMPLE\r\n" + "\r\n" + "{'user': 'steveen'}Garbage data in here",
			wants: []string{"{'user': 'steveen'}"},
		},
		{
			input: "Content-Length: 0\r\n" + "Content-Type: utf8\r\n" + "Authorization: SIMPLE\r\n" + "\r\n" + "{'user': 'steveen'}Garbage data in here",
			wants: []string{""},
		},
		{
			input: "Content-Length: -19\r\n" + "Content-Type: utf8\r\n" + "Authorization: SIMPLE\r\n" + "\r\n" + "{'user': 'steveen'}Garbage data in here",
			wants: []string{""},
		},
		{
			input: "Content-Length: 19\r\n" + "Content-Type: utf8\r\n" + "Authorization: SIMPLE\r\n" + 
				"\r\n" + "{'user': 'steveen'}" + 
				"Content-Length: 10\r\n" + "Content-Type: utf8\r\n" + "\r\n" + "{'num': 3}",
			wants: []string{"{'user': 'steveen'}", "{'num': 3}"},
		},
		{
			input: "Content-Length: 19\r\n" + "Content-Type: utf8\r\n" + "Authorization: SIMPLE\r\n" + 
				"\r\n" + "{'user': 'steveen'}" + 
				"Content-Length: 10\r\n" + "Content-Type: utf8\r\n" + "\r\n" + "{'num': 3}",
			wants: []string{"{'user': 'steveen'}", "{'num': 3}"},
		},
		{
			input: "Content-Length: 19\r\n" + "Content-Type: utf8\r\n" + "Authorization: SIMPLE\r\n" + 
				"\r\n" + "{'user': 'steveen'}Garbage data in here" + 
				"Content-Length: 10\r\n" + "Content-Type: utf8\r\n" + "\r\n" + "{'num': 3}",
			wants: []string{"{'user': 'steveen'}", "{'num': 3}"},
		},
		{
			input: "Content-Type: utf8\r\n" + "Authorization: SIMPLE\r\n" + "Content-Length: 19\r\n" + 
				"\r\n" + "{'user': 'steveen'}" + 
				"Content-Type: utf8\r\n" + "Content-Length: 10\r\n" + "\r\n" + "{'num': 3}",
			wants: []string{"{'user': 'steveen'}", "{'num': 3}"},
		},
		{
			input: "\r\n" + 
				"\r\n" + "{'user': 'steveen'}" + 
				"Content-Type: utf8\r\n" + "Content-Length: 10\r\n" + "\r\n" + "{'num': 3}",
			wants: []string{"", "{'num': 3}"},
		},
	}

	for count, test := range tests {
		testName := strconv.Itoa(count) + "_" + test.input[:8] + "_test"

		t.Run(testName, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(test.input))
			scanner.Split(inputParsingSplitFunc)

			answers := []string{}

			for scanner.Scan() {
				msg := scanner.Text()
				answers = append(answers, msg)
			}

			isError := scanner.Err() != nil

			if len(answers) != len(test.wants) {
				t.Errorf("\n Size of answers (%d) do not match the expected one (%d)", len(answers), len(test.wants))
				t.Errorf("\n Expected: %v \n Got: %v \n Error: %v", test.wants, answers, scanner.Err())
				return
			}

			for i := 0; i < len(answers); i++ {
				if answers[i] != test.wants[i] || isError != test.isError {
					t.Errorf("\n Expected : %s \n Got : %s \n With error: %v", test.wants[i], answers[i], scanner.Err())
				} 
			}
		})
	}
}
