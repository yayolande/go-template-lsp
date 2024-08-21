package main

import (
	"bufio"
	"bytes"
	"errors"
	"math"

	// "encoding/binary"
	"fmt"
	"strconv"

	// "math/big"
	"os"
	"path/filepath"
	// "strings"
	// "strings"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Split(generateScanLspSplitter())

	cwd, _ := os.Getwd()
	logfileName := "log_output.txt"
	w, err := os.Create(filepath.Join(cwd, logfileName))

	if err != nil {
		panic("Error: " + err.Error())
	}

	defer w.Close()

	for {
		scanner.Scan()
		message := scanner.Text()
		fmt.Fprintln(w, "message captured: ", message)
		// fmt.Fprintln(w, os.Environ())

		if scanner.Err() != nil {
			fmt.Fprintln(w, "error: ", scanner.Err().Error())
		}
	}
}

func generateScanLspSplitter() bufio.SplitFunc {
	var messageBodyLength int = math.MaxInt

	return func (data []byte, atEOF bool) (advance int, token []byte, err error) {
		w, errOther := os.OpenFile("split_function_log.txt", os.O_APPEND | os.O_CREATE | os.O_WRONLY, 0644)
		fmt.Fprintln(w, "split_function_data: ", string(data))

		// if bytes.Contains(data, []byte("\r\n\r\n")) {
		// index := bytes.Index(data, []byte("\r\n\r\n"))
		index := bytes.Index(data, []byte("\r\n"))

		fmt.Println("Global -- Index: ", index, " :: len(data) = ", len(data), " :: contentLengthBefore = ", messageBodyLength)

		if index != -1 && index != 0 {
			line := data[:index]

			if bytes.Contains(line, []byte("Content-Length")) {
				fmt.Fprintln(w, "Content-Length content -- ", string(line))

				indexSeperator := bytes.Index(line, []byte(":")) + 1
				contentLenght := line[indexSeperator:]
				contentLenght, _ = bytes.CutSuffix(contentLenght, []byte("\r\n"))	// ???? Pointless
				contentLenght = bytes.TrimSpace(contentLenght)

				messageBodyLength, errOther = strconv.Atoi(string(contentLenght))
				if errOther != nil {
					msg := "Error, while converting to int: " + errOther.Error()
					fmt.Fprintln(w, msg)

					fmt.Fprintln(w, "============================")
					return index + 2, nil, errors.New(msg)
				}
			}

			if bytes.Contains(line, []byte("Content-Type")) {
				fmt.Fprintln(w, "Content-Type content -- ", string(line))
			}

			fmt.Fprintln(w, "============================ index != -1 && index != 1")
			return index + 2, nil, nil
		}
		
		if index != -1 && index == 0 {
			/*
			contentLenght := data[:index]
			contentLenght, _ = bytes.CutPrefix(contentLenght, []byte("Content-Length:"))
			contentLenght, _ = bytes.CutSuffix(contentLenght, []byte("\r\n"))
			contentLenght = bytes.TrimSpace(contentLenght)

			messageBodyLength, err := strconv.Atoi(string(contentLenght))
			if err != nil {
				fmt.Fprintln(w, " ------ error while converting to int: ", err.Error(), " ---")
			}
			*/

			// fmt.Fprintln(w, "--------------- Content-Length: ", string(contentLenght), " -- Or length: ", messageBodyLength)

			// return index + 4, data[:index], nil
			_, errOther := fmt.Fprintln(w, "============================ Found index = 1")
			if errOther != nil {
				fmt.Println("error with found index = 1 :: ", errOther.Error())
				return 0, nil, errOther
			}
			fmt.Println("===== found index = 1, split_func error = ", err)
			return index + 2, nil, nil
		}

		if len(data) >= messageBodyLength {
			fmt.Fprintln(w, "body -- ", data[:messageBodyLength])

			fmt.Fprintln(w, "============================ len(data) >= messageBodyLength")
			return messageBodyLength, data[:messageBodyLength], nil
		} else {
			fmt.Fprint(w, "unreached code: data_lenght = ", len(data))
		}

		fmt.Fprintln(w, "============================")
		return 0, nil, nil
	}
}

