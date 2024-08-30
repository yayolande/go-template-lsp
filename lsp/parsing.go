package lsp

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"
	// "os"
)

// func Decode (in *os.File) *bufio.Scanner {
func Decode (input io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(input)
	scanner.Split(inputParsingSplitFunc)

	return scanner
}

func inputParsingSplitFunc (data []byte, atEOF bool) (advance int, token []byte, err error) {
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
