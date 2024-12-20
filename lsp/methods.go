package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/yayolande/gota"
	"github.com/yayolande/gota/lexer"
	checker "github.com/yayolande/gota/analyzer"
)

var filesOpenedByEditor = make(map[string]string)

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

type NotificationMessage [T any] struct {
	JsonRpc	string `json:"jsonrpc"`
	Method string `json:"method"`
	Params	T	`json:"params"`
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
	DefinitionProvider bool `json:"definitionProvider"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities	`json:"capabilities"`
	ServerInfo	struct{
		Name	string	`json:"name"`
		Version	string	`json:"version"`
	}	`json:"serverInfo"`
}

// Notification publish Diagnostics Params
type PublishDiagnosticsParams struct {
	Uri	string	`json:"uri"`
	Diagnostics	[]Diagnostic	`json:"diagnostics"`
}

type Diagnostic struct {
	Range	Range	`json:"range"`
	Message	string	`json:"message"`
}

func convertParserRangeToLspRange(parserRange lexer.Range) Range {
	reach := Range{}

	reach.Start.Line = uint(parserRange.Start.Line)
	reach.Start.Character = uint(parserRange.Start.Character)

	reach.End.Line = uint(parserRange.End.Line)
	reach.End.Character = uint(parserRange.End.Character)

	return reach
}

func ProcessInitializeRequest (data []byte) (response []byte, root string) {
	req := RequestMessage[InitializeParams]{}

	err := json.Unmarshal(data, &req)
	if err != nil {
		log.Printf("fatal, unmarshalling failed. \n request = %#v \n", data)
		panic("error while unmarshalling data during 'initialize' phase, " + err.Error())
	}

	res := ResponseMessage[InitializeResult] {
		JsonRpc: "2.0",
		Id: req.Id,
		Result: InitializeResult{
			Capabilities: ServerCapabilities{
				TextDocumentSync: 1,
				HoverProvider: true,
				DefinitionProvider: true,
			},
		},
	}

	res.Result.ServerInfo.Name = "steveen_server"
	res.Result.ServerInfo.Version = "0.1.0"

	response, err = json.Marshal(res)
	if err != nil {
		log.Printf("fatal, marshalling failed \n response = %#v", res)
		panic("error while 'marshalling' data during 'initialize' phase, " + err.Error())
	}

	root = req.Params.RootUri

	return response, root
}

func ProcessInitializedNotificatoin(data []byte) {
	// This is intentionally left empty since the LSP documentation do not describe anything
	// The only reason for this notification (for now) is to register new server capabilities
	// [Read more](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#initialized)
	log.Println("Succesfully received 'initialized' notification")
}

type TextDocumentItem struct {
	Uri	string	`json:"uri"`
	Version	int	`json:"version"`
	LanguageId	string	`json:"languageId"`
	Text	string	`json:"text"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem	`json:"textDocument"`
}

func ProcessDidOpenTextDocumentNotification (data []byte) (fileURI string, fileContent []byte) {
	request := RequestMessage[DidOpenTextDocumentParams]{}

	err := json.Unmarshal(data, &request)
	if err != nil {
		log.Printf("fatal, unmarshalling failed. \n request = %#v \n", data)
		panic("error while unmarshalling data during 'textDocument/didOpen' phase, " + err.Error())
	}

	documentURI := request.Params.TextDocument.Uri
	documentContent := request.Params.TextDocument.Text
	filesOpenedByEditor[documentURI] = documentContent

	log.Printf("\n ======= filesOpenedByEditor: %+v \n ======= \n", filesOpenedByEditor)

	return documentURI, []byte(documentContent)
}

type Position struct {
	Line	uint	`json:"line"`
	Character uint	`json:"character"`
}

type Range struct {
	Start	Position	`json:"start"`
	End	Position	`json:"end"`
}

type TextDocumentContentChangeEvent struct {
	Range Range	`json:"range"`
	RangeLength	uint	`json:"rangeLength"`
	Text	string	`json:"text"`
}

type DidChangeTextDocumentParams struct {
	TextDocument	TextDocumentItem	`json:"textDocument"`
	ContentChanges	[]TextDocumentContentChangeEvent	`json:"contentChanges"`
}

func ProcessDidChangeTextDocumentNotification (data []byte) (fileURI string, fileContent []byte) {
	var request RequestMessage[DidChangeTextDocumentParams]

	err := json.Unmarshal(data, &request)
	if err != nil {
		log.Printf("fatal, unmarshalling failed. \n request = %#v \n", data)
		panic("error while unmarshalling data during 'textDocument/didChange' phase, " + err.Error())
	}

	documentChanges := request.Params.ContentChanges
	if len(documentChanges) > 1 {
		log.Printf("fatal, unexpected data type (incremental change instead of full text). \n request = %#v\n", request)
		panic("the server can't handle incremental change yet in 'textDocument/didChange'. " +
			"register the correct server capabilities in 'initialize' phase")
	}

	if len(documentChanges) == 0 {
		log.Printf("error detected from client request. 'documentChanges' field cannot be empty. \n request = %#v", request)
		return "", nil
	}

	documentContent := documentChanges[0].Text
	documentURI := request.Params.TextDocument.Uri
	filesOpenedByEditor[documentURI] = documentContent

	log.Printf("\n ======= filesOpenedByEditor: %+v \n ======= \n", filesOpenedByEditor)

	return documentURI, []byte(documentContent)
}

type DidCloseTextDocumentParams struct {
	TextDocument	TextDocumentItem	`json:"textDocument"`
}

func ProcessDidCloseTextDocumentNotification (data []byte) (fileURI string, fileContent []byte) {
	var request RequestMessage[DidCloseTextDocumentParams]

	err := json.Unmarshal(data, &request)
	if err != nil {
		log.Printf("fatal, unmarshalling failed. \n request = %#v \n", data)
		panic("error while unmarshalling data during 'textDocument/didClose' phase, " + err.Error())
	}

	documentPath := request.Params.TextDocument.Uri
	documentContent := request.Params.TextDocument.Text
	delete(filesOpenedByEditor, documentPath)

	log.Printf("\n ======= filesOpenedByEditor: %+v \n ======= \n", filesOpenedByEditor)

	return documentPath, []byte(documentContent)
}

type MarkupContent struct {
	Kind	string	`json:"kind"`
	Value	string	`json:"value"`
}

func ProcessHoverRequest (data []byte) []byte {
	type HoverParams struct {
		TextDocument	TextDocumentItem	`json:"textDocument"`
		Position	Position	`json:"position"`
	}

	var request RequestMessage[HoverParams]

	err := json.Unmarshal(data, &request)
	if err != nil {
		log.Println("Error, unable unmarshal DidOpenTExtDocument Notification: ", err.Error())
		// TODO: return appropriate error message
		return nil
	}

	documentPath := request.Params.TextDocument.Uri
	documentContent := filesOpenedByEditor[documentPath]
	countLine := int(request.Params.Position.Line)
	countChar := int(request.Params.Position.Character)

	scanner := bufio.NewScanner(strings.NewReader(documentContent))
	for i := 0; i <= countLine; i++ {
		scanner.Scan()
	}

	line := scanner.Text()

	log.Printf("====> line: %+v :::: characterCount: %d", line, countChar)

	var word string

	character := line[countChar]
	indexCursor := bytes.ContainsAny([]byte{character}, " ,.-<>/\\")
	word = string(character)

	if ! indexCursor {
		indexRightSeparator := strings.IndexAny(line[countChar:], " ,.-<>/\\")
		indexLeftSeparator := strings.LastIndexAny(line[:countChar], " ,.-<>/\\")

		if indexLeftSeparator == -1 {
			indexLeftSeparator = -1
		}

		if indexRightSeparator == -1 {
			indexRightSeparator = len(line)
		} else {
			indexRightSeparator += countChar
		}

		word = line[indexLeftSeparator + 1 : indexRightSeparator]
	}


	type HoverResult struct {
		Contents	MarkupContent	`json:"contents"`
		Range	Range	`json:"range,omitempty"`
	}
	
	response := ResponseMessage[HoverResult]{
		JsonRpc: request.JsonRpc,
		Id: request.Id,
		Result: HoverResult{
			Contents: MarkupContent{
				Kind: "plaintext",
				Value: fmt.Sprintf("%s -- LSP", word),
				// Value: fmt.Sprintf("%c -- LSP", character),
			},
			Range: Range{
			},
		},
	}

	responseText, err := json.Marshal(response)
	if err != nil {
		log.Println("Error while marshalling ResponseMessageHoverResult: ", err.Error())
		// TODO: Need to better handle error case
		return nil
	}

	return responseText
}

type TextDocumentIdentifier struct {
	Uri	string	`json:"uri"`
}

type TextDocumentPositionParams struct {
	TextDocument	TextDocumentIdentifier `json:"textDocument"`
	Position			Position `json:"position"`
}

type Location struct {
	Uri	string `json:"uri"`
	Range	Range	`json:"range"`
}

// TODO: this implementation work so well that it should be propagated to the other 'Params'
type DefinitionParams struct {
	TextDocumentPositionParams
}

type DefinitionResults struct {
	Location
}

func ProcessGoToDefinition(data []byte, openFiles map[string]*checker.FileDefinition) []byte {
	if len(openFiles) == 0 {
		panic("cannot compute the source of 'go-to-definition' because no files have been opened on the server")
	}

	var req RequestMessage[DefinitionParams]

	err := json.Unmarshal(data, &req)
	if err != nil {
		log.Println("error while decoding/unmarshalling lsp client data, ", err.Error())
		return nil
	}

	position := lexer.Position{
		Line: int(req.Params.Position.Line),
		Character: int(req.Params.Position.Character),
	}

	file := openFiles[req.Params.TextDocument.Uri]
	if file == nil {
		panic("file requested by lsp client is not open on the server. that file must be open for 'go-to-definition' to make any computation")
	}

	fileNameURI, reach := gota.GoToDefinition(file, position)

	var res ResponseMessage[[]DefinitionResults]

	res.Id = req.Id
	res.JsonRpc = req.JsonRpc

	result := DefinitionResults{}
	result.Uri = fileNameURI
	result.Range = convertParserRangeToLspRange(reach)

	res.Result = append(res.Result, result)
	if fileNameURI == "" {
		res.Result = nil
	}

	data, err = json.Marshal(res)
	if err != nil {
		log.Println("error while encoding/marshalling data for lsp client, ", err.Error())
		return nil
	}

	log.Printf("definition found : %#v\n", res)

	return data
}

