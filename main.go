package main

import (
	"encoding/json"
	"log"
	"os"
	// "strings"

	"go-template-lsp/lsp"
)

func main() {

	// str := "Content-Length: 865\r\n\r\n" + `{"method":"initialize","jsonrpc":"2.0","id":1,"params":{"workspaceFolders":null,"capabilities":{"textDocument":{"completion":{"dynamicRegistration":false,"completionList":{"itemDefaults":["commitCharacters","editRange","insertTextFormat","insertTextMode","data"]},"contextSupport":true,"completionItem":{"snippetSupport":true,"labelDetailsSupport":true,"insertTextModeSupport":{"valueSet":[1,2]},"resolveSupport":{"properties":["documentation","detail","additionalTextEdits","sortText","filterText","insertText","textEdit","insertTextFormat","insertTextMode"]},"insertReplaceSupport":true,"tagSupport":{"valueSet":[1]},"preselectSupport":true,"deprecatedSupport":true,"commitCharactersSupport":true},"insertTextMode":1}}},"rootUri":null,"rootPath":null,"clientInfo":{"version":"0.10.1+v0.10.1","name":"Neovim"},"processId":230750,"workDoneToken":"1","trace":"off"}}`

	// scanner := bufio.NewScanner(strings.NewReader(str))
	// scanner := bufio.NewScanner(os.Stdin)
	// scanner.Split(inputParsingSplitFunc)
	configureLogging()

	// scanner := lsp.Decode(strings.NewReader(str))
	scanner := lsp.Decode(os.Stdin)

	var request lsp.RequestMessage[any]
	var response []byte

	for scanner.Scan() {
		data := scanner.Bytes()

		json.Unmarshal(data, &request)
		log.Printf("Received json struct: %+v\n", request)

		switch request.Method {
		case "initialize":
			response := lsp.ProcessInitializeRequest(data)

			_, err := os.Stdout.Write(response)
			if err != nil {
				log.Printf("Error while writing file to 'stdout': ", err.Error())
			}
		}

		log.Printf("Sent json struct: %+v", response)
		response = []byte{}
	}

	if scanner.Err() != nil {
		log.Printf("error: ", scanner.Err().Error())
	}

	log.Printf("\n Shutting down custom lsp server")
}

func configureLogging() {
	logfileName := "log_output.txt"
	file, err := os.Create(logfileName)
	if err != nil {
		panic("Error: " + err.Error())
	}

	// logger := log.New(file, " :: ", log.Ldate | log.Ltime | log.Lshortfile)
	log.SetPrefix(" --> ")
	log.SetOutput(file)
}



