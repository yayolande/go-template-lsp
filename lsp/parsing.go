package lsp

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"
	"log"
)

// func Decode (in *os.File) *bufio.Scanner {
func ReceiveInput (input io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(input)
	scanner.Split(decode)

	return scanner
}

// For most case, 'SendToLspClient()' is prefered since it automatically encode the response.
// Send response data over output. The respose data must be 'enconded' first
func SendOutput (output io.Writer, response []byte) {
	_, err := output.Write(response)
	if err != nil {
		log.Printf("Error while writing file to 'stdout': ", err.Error())
	}
}

// Send 'response' to LSP client over the wire ('output'). 
// Encoding is done within this function, so it is not advised to do another encoding
func SendToLspClient(output io.Writer, response []byte) {
	response = Encode(response)
	SendOutput(output, response)
}

func Encode (dataContent []byte) []byte {
	length := strconv.Itoa(len(dataContent))
	dataHeader := []byte("Content-Length: " + length + "\r\n\r\n")
	data := append(dataHeader, dataContent...)

	return data
}

// func inputParsingSplitFunc (data []byte, atEOF bool) (advance int, token []byte, err error) {
func decode (data []byte, atEOF bool) (advance int, token []byte, err error) {
	indexStartData := bytes.Index(data, []byte("\r\n\r\n"))
	if indexStartData == - 1 {
		return 0, nil, nil
	}

	// TODO: Support 'Content-Type' header as well in the near future
	contentLength, err := getHeaderContentLengthSplitFunc(data[:indexStartData])
	if err != nil {
		return indexStartData + 4, []byte{}, nil
	}

	if len(data[indexStartData:]) < contentLength {
		return 0, nil, nil
	}

	indexStartData = indexStartData + 4
	indexEndData := indexStartData + contentLength

	return indexEndData, data[indexStartData:indexEndData], nil
}

func getHeaderContentLengthSplitFunc(data []byte) (int, error) {
	indexHeader := bytes.LastIndex(data, []byte("Content-Length"))
	if indexHeader == -1 {
		return -1, errors.New("Unable to find the 'content-length' for this input ! Input parsing aborted" )
	}

	indexLineSeparator := bytes.Index(data[indexHeader:], []byte("\r\n"))
	if indexLineSeparator >= 0 {
		indexLineSeparator += indexHeader
	} else if indexLineSeparator == -1 {
		indexLineSeparator = len(data)
	}

	indexKeyValueSeparator := bytes.Index(data[indexHeader:indexLineSeparator], []byte(":"))
	if indexKeyValueSeparator == -1 {
		return -1, errors.New("Malformated 'Content-Length' ! Unable to find kay-value pair separator ':'")
	}

	indexKeyValueSeparator += indexHeader

	contentLengthString := data[indexKeyValueSeparator + 1 : indexLineSeparator]
	contentLengthString = bytes.TrimSpace(contentLengthString)

	contentLength, err := strconv.Atoi(string(contentLengthString))
	if err != nil {
		return -1, errors.New("Malformated 'Content-Length' ! content length value is not an integer")
	}

	if contentLength < 0 {
		return -1, errors.New("Error, 'Content-Length' shouldn't have a negative value")
	}

	return contentLength, nil
}
