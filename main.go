package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func main() {

	// str := "Content-Length: 865\r\n\r\n" + `{"method":"initialize","jsonrpc":"2.0","id":1,"params":{"workspaceFolders":null,"capabilities":{"textDocument":{"completion":{"dynamicRegistration":false,"completionList":{"itemDefaults":["commitCharacters","editRange","insertTextFormat","insertTextMode","data"]},"contextSupport":true,"completionItem":{"snippetSupport":true,"labelDetailsSupport":true,"insertTextModeSupport":{"valueSet":[1,2]},"resolveSupport":{"properties":["documentation","detail","additionalTextEdits","sortText","filterText","insertText","textEdit","insertTextFormat","insertTextMode"]},"insertReplaceSupport":true,"tagSupport":{"valueSet":[1]},"preselectSupport":true,"deprecatedSupport":true,"commitCharactersSupport":true},"insertTextMode":1}}},"rootUri":null,"rootPath":null,"clientInfo":{"version":"0.10.1+v0.10.1","name":"Neovim"},"processId":230750,"workDoneToken":"1","trace":"off"}}`

	// scanner := bufio.NewScanner(strings.NewReader(str))
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Split(inputParsingSplitFunc)

	logfileName := "log_output.txt"
	cwd, _ := os.Getwd()
	w, err := os.Create(filepath.Join(cwd, logfileName))
	if err != nil {
		panic("Error: " + err.Error())
	}

	defer w.Close()

	type RequestMessage[T any] struct {
		JsonRpc	string `json:"jsonrpc"`
		Id	int	`json:"id"`
		Method string `json:"method"`
		Params	T	`json:"params"`
	}


	var message RequestMessage[any]

	for scanner.Scan() {
		data := scanner.Bytes()

		json.Unmarshal(data, &message)
		fmt.Fprintf(w, "json struct: %+v\n", message)
	}

	if scanner.Err() != nil {
		fmt.Fprintln(w, "error: ", scanner.Err().Error())
	}

	fmt.Fprintln(w, "\n Shutting down custom lsp server")
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

