package lsp

import (
	"log"
	"encoding/json"
	"strconv"
)

type RequestMessage[T any] struct {
	JsonRpc	string `json:"jsonrpc"`
	Id	int	`json:"id"`
	Method string `json:"method"`
	Params	T	`json:"params"`
}

type ResponseMessage [T any] struct {
	JsonRpc	string `json:"jsonrpc"`
	Id	int	`json:"id"`
	Result	T	`json:"result"`
}

type InitializeParams struct {
	ProcessId	int	`json:"processId"`
	Capabilities map[string]interface{}	`json:"capabilities"`
	ClientInfo	struct {
		Name	string	`json:"name"`
		Version	string	`json:"version"`
	}	`json:"clientInfo"`
	Locale	string	`json:"locale"`
	RootUri	string	`json:"rootUri"`
	Trace	any	`json:"trace"`
	WorkspaceFolders	any `json:"workspaceFolders"`
	InitializationOptions	any	`json:"initializationOptions"`
}

type ServerCapabilities struct {
	TextDocumentSync	int	`json:"textDocumentSync"`
	HoverProvider	bool	`json:"hoverProvider"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities	`json:"capabilities"`
	ServerInfo	struct{
		Name	string	`json:"name"`
		Version	string	`json:"version"`
	}	`json:"serverInfo"`
}

func ProcessInitializeRequest (data []byte) []byte {
	req := RequestMessage[InitializeParams]{}

	err := json.Unmarshal(data, &req)
	if err != nil {
		log.Println("Error while unmarshalling data during 'initialize' phase: ", err.Error())
		return nil
	}

	// log.Printf("\n requestMessageInitialize: %+v \n", req)

	response := ResponseMessage[InitializeResult] {
		JsonRpc: "2.0",
		Id: req.Id,
		Result: InitializeResult{
			Capabilities: ServerCapabilities{
				TextDocumentSync: 1,
				HoverProvider: true,
			},
		},
	}

	response.Result.ServerInfo.Name = "steveen_server"
	response.Result.ServerInfo.Version = "0.1.0"

	responseText, err := json.Marshal(response)
	if err != nil {
		log.Printf("Error while marshalling : ", err.Error())
	}

	lengthBody := strconv.Itoa(len(responseText))
	responseHeader := []byte("Content-Length: " + lengthBody + "\r\n\r\n")
	responseText = append(responseHeader, responseText...)

	// log.Printf("\n Server response to init: %+v", string(responseText))

	return responseText
}
