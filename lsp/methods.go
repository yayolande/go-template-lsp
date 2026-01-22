package lsp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"sync"

	"github.com/yayolande/gota"
	checker "github.com/yayolande/gota/analyzer"
	"github.com/yayolande/gota/lexer"
	"github.com/yayolande/gota/parser"
)

var filesOpenedByEditor = make(map[string]string)

type WorkSpaceStore struct {
	RootPath          string
	RawFiles          map[string][]byte
	ParsedFiles       map[string]*parser.GroupStatementNode
	ErrorsParsedFiles map[string][]lexer.Error

	OpenedFilesAnalyzed map[string]*checker.FileDefinition
	ErrorsAnalyzedFiles map[string][]lexer.Error
}

type ID int

func (id *ID) UnmarshalJSON(data []byte) error {
	length := len(data)
	if data[0] == '"' && data[length-1] == '"' {
		data = data[1 : length-1]
	}

	number, err := strconv.Atoi(string(data))
	if err != nil {
		return fmt.Errorf("'ID' expected either a string or an integer only")
	}

	*id = ID(number)
	return nil
}

func (id *ID) MarshalJSON() ([]byte, error) {
	val := strconv.Itoa(int(*id))
	return []byte(val), nil
}

type RequestMessage[T any] struct {
	JsonRpc string `json:"jsonrpc"`
	Id      ID     `json:"id"`
	Method  string `json:"method"`
	Params  T      `json:"params"`
}

type ResponseMessage[T any] struct {
	JsonRpc string         `json:"jsonrpc"`
	Id      ID             `json:"id"`
	Result  T              `json:"result"`
	Error   *ResponseError `json:"error"`
}

type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type NotificationMessage[T any] struct {
	JsonRpc string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  T      `json:"params"`
}

type InitializeParams struct {
	ProcessId    int            `json:"processId"`
	Capabilities map[string]any `json:"capabilities"`
	ClientInfo   struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
	Locale                string `json:"locale"`
	RootUri               string `json:"rootUri"`
	Trace                 any    `json:"trace"`
	WorkspaceFolders      any    `json:"workspaceFolders"`
	InitializationOptions any    `json:"initializationOptions"`
}

type ServerCapabilities struct {
	TextDocumentSync     int  `json:"textDocumentSync"`
	HoverProvider        bool `json:"hoverProvider"`
	DefinitionProvider   bool `json:"definitionProvider"`
	FoldingRangeProvider bool `json:"foldingRangeProvider"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// Notification publish Diagnostics Params
type PublishDiagnosticsParams struct {
	Uri         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Message  string `json:"message"`
	Severity int    `json:"severity"`
}

func convertParserRangeToLspRange(parserRange lexer.Range) Range {
	if parserRange.IsEmpty() {
		return Range{}
	}

	reach := Range{}
	reach.Start.Line = uint(parserRange.Start.Line)
	reach.Start.Character = uint(parserRange.Start.Character)

	reach.End.Line = uint(parserRange.End.Line)
	reach.End.Character = uint(parserRange.End.Character)

	return reach
}

func ProcessInitializeRequest(data []byte, lspName string, lspVersion string) (response []byte, root string) {
	req := RequestMessage[InitializeParams]{}

	err := json.Unmarshal(data, &req)
	if err != nil {
		msg := ("error while unmarshalling data during 'initialize' phase, " + err.Error())
		slog.Error(msg,
			slog.Group("details",
				slog.Any("unmarshalled_req", req),
				slog.String("received_req", string(data)),
			),
		)
		panic(msg)
	}

	res := ResponseMessage[InitializeResult]{
		JsonRpc: "2.0",
		Id:      req.Id,
		Result: InitializeResult{
			Capabilities: ServerCapabilities{
				TextDocumentSync:     1,
				HoverProvider:        true,
				DefinitionProvider:   true,
				FoldingRangeProvider: true,
			},
		},
	}

	res.Result.ServerInfo.Name = lspName
	res.Result.ServerInfo.Version = lspVersion

	response, err = json.Marshal(res)
	if err != nil {
		msg := ("error while 'marshalling' data during 'initialize' phase, " + err.Error())
		slog.Error(msg)
		panic(msg)
	}

	root, err = url.PathUnescape(req.Params.RootUri) // needed for windows os
	if err != nil {
		slog.Error("root uri received from client is malformated. "+err.Error(),
			slog.Group("details",
				slog.String("root_uri", req.Params.RootUri),
				slog.Any("request_info", req),
			),
		)
		root = "file://root_uri_malformated_error"
	}

	return response, root
}

func ProcessInitializedNotificatoin(data []byte) {
	// This is intentionally left empty since the LSP documentation do not describe anything
	// The only reason for this notification (for now) is to register new server capabilities
	// [Read more](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#initialized)
	slog.Info("Succesfully received 'initialized' notification", slog.String("data", string(data)))
}

func ProcessShutdownRequest(jsonVersion string, requestId ID) []byte {
	response := ResponseMessage[any]{
		JsonRpc: jsonVersion,
		Id:      requestId,
		Result:  nil,
		Error:   nil,
	}

	responseText, err := json.Marshal(response)
	if err != nil {
		msg := ("Error while marshalling ProcessShutdownRequest: " + err.Error())
		slog.Error(msg)
		panic(msg)
	}

	return responseText
}

func ProcessIllegalRequestAfterShutdown(jsonVersion string, requestId ID) []byte {
	response := ResponseMessage[any]{
		JsonRpc: jsonVersion,
		Id:      requestId,
		Result:  nil,
		Error: &ResponseError{
			Code:    -32600,
			Message: "illegal request while server shutting down",
		},
	}

	responseText, err := json.Marshal(response)
	if err != nil {
		msg := ("Error while marshalling ProcessIllegalRequestAfterShutdown(): " + err.Error())
		slog.Error(msg)
		panic(msg)
	}

	return responseText
}

type TextDocumentItem struct {
	Uri        string `json:"uri"`
	Version    int    `json:"version"`
	LanguageId string `json:"languageId"`
	Text       string `json:"text"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

func ProcessDidOpenTextDocumentNotification(data []byte) (fileURI string, fileContent []byte) {
	request := RequestMessage[DidOpenTextDocumentParams]{}

	err := json.Unmarshal(data, &request)
	if err != nil {
		msg := ("error while unmarshalling data during 'textDocument/didOpen' phase, " + err.Error())
		slog.Error(msg,
			slog.Group("details",
				slog.Any("unmarshalled_req", request),
				slog.String("received_req", string(data)),
			),
		)
		panic(msg)
	}

	documentURI, err := url.PathUnescape(request.Params.TextDocument.Uri) // needed for windows os
	if err != nil {
		slog.Error("file uri received from client is malformated. "+err.Error(),
			slog.Group("details",
				slog.String("file_uri", request.Params.TextDocument.Uri),
				slog.Any("request_info", request),
			),
		)
		documentURI = request.Params.TextDocument.Uri
	}

	documentContent := request.Params.TextDocument.Text
	filesOpenedByEditor[documentURI] = documentContent

	return documentURI, []byte(documentContent)
}

type Position struct {
	Line      uint `json:"line"`
	Character uint `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type TextDocumentContentChangeEvent struct {
	Range       Range  `json:"range"`
	RangeLength uint   `json:"rangeLength"`
	Text        string `json:"text"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   TextDocumentItem                 `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

func ProcessDidChangeTextDocumentNotification(data []byte) (fileURI string, fileContent []byte) {
	var request RequestMessage[DidChangeTextDocumentParams]

	err := json.Unmarshal(data, &request)
	if err != nil {
		msg := ("error while unmarshalling data during 'textDocument/didChange' phase, " + err.Error())
		slog.Error(msg,
			slog.Group("details",
				slog.Any("unmarshalled_req", request),
				slog.String("received_req", string(data)),
			),
		)
		panic(msg)
	}

	documentChanges := request.Params.ContentChanges
	if len(documentChanges) > 1 {
		msg := ("the server can't handle incremental change yet in 'textDocument/didChange'. " +
			"register the correct server capabilities in 'initialize' phase")
		slog.Error(msg,
			slog.Group("details",
				slog.Any("unmarshalled_req", request),
				slog.String("received_req", string(data)),
			),
		)
		panic(msg)
	}

	if len(documentChanges) == 0 {
		slog.Warn("error detected from client request. 'documentChanges' field cannot be empty")
		return "", nil
	}

	documentURI, err := url.PathUnescape(request.Params.TextDocument.Uri) // needed for windows os
	if err != nil {
		slog.Error("file uri received from client is malformated. "+err.Error(),
			slog.Group("details",
				slog.String("file_uri", request.Params.TextDocument.Uri),
				slog.Any("request_info", request),
			),
		)
		documentURI = request.Params.TextDocument.Uri
	}

	documentContent := documentChanges[0].Text
	filesOpenedByEditor[documentURI] = documentContent

	return documentURI, []byte(documentContent)
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

func ProcessDidCloseTextDocumentNotification(data []byte) (fileURI string, fileContent []byte) {
	var request RequestMessage[DidCloseTextDocumentParams]

	err := json.Unmarshal(data, &request)
	if err != nil {
		msg := ("error while unmarshalling data during 'textDocument/didClose' phase, " + err.Error())
		slog.Error(msg,
			slog.Group("details",
				slog.Any("unmarshalled_req", request),
				slog.String("received_req", string(data)),
			),
		)
		panic(msg)
	}

	documentPath, err := url.PathUnescape(request.Params.TextDocument.Uri) // needed for windows os
	if err != nil {
		slog.Error("file uri received from client is malformated. "+err.Error(),
			slog.Group("details",
				slog.String("file_uri", request.Params.TextDocument.Uri),
				slog.Any("request_info", request),
			),
		)
		documentPath = request.Params.TextDocument.Uri
	}

	documentContent := request.Params.TextDocument.Text
	delete(filesOpenedByEditor, documentPath)

	return documentPath, []byte(documentContent)
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func ProcessHoverRequest(data []byte, openFiles map[string]*checker.FileDefinition) []byte {
	type HoverParams struct {
		TextDocument TextDocumentItem `json:"textDocument"`
		Position     Position         `json:"position"`
	}

	var request RequestMessage[HoverParams]

	err := json.Unmarshal(data, &request)
	if err != nil {
		slog.Warn("Error, unable unmarshal DidOpenTExtDocument Notification: " + err.Error())
		// TODO: return appropriate error message
		return nil
	}

	position := lexer.Position{
		Line:      int(request.Params.Position.Line),
		Character: int(request.Params.Position.Character),
	}

	fileUri, err := url.PathUnescape(request.Params.TextDocument.Uri) // needed for windows os
	if err != nil {
		slog.Error("file uri received from client is malformated. "+err.Error(),
			slog.Group("details",
				slog.String("file_uri", request.Params.TextDocument.Uri),
				slog.Any("request_info", request),
			),
		)
		fileUri = request.Params.TextDocument.Uri
	}

	file := openFiles[fileUri]
	if file == nil {
		msg := ("file requested by lsp client is not open on the server. that file must be open for 'go-to-definition' to make any computation")
		slog.Error(msg,
			slog.Group("details",
				slog.String("uri", request.Params.TextDocument.Uri),
				slog.Any("unmarshalled_req", request),
				slog.String("received_req", string(data)),
			),
		)
		panic(msg)
	}

	typeStringified, reach := gota.Hover(file, position)

	type HoverResult struct {
		Contents MarkupContent `json:"contents"`
		Range    Range         `json:"range,omitempty"`
	}

	response := ResponseMessage[*HoverResult]{
		JsonRpc: request.JsonRpc,
		Id:      request.Id,
		Result: &HoverResult{
			Contents: MarkupContent{
				Kind:  "markdown",
				Value: typeStringified,
			},
			Range: convertParserRangeToLspRange(reach),
		},
	}

	if typeStringified == "" {
		response.Result = nil
	}

	responseText, err := json.Marshal(response)
	if err != nil {
		slog.Warn("Error while marshalling ResponseMessageHoverResult: " + err.Error())
		// TODO: Need to better handle error case
		return nil
	}

	return responseText
}

type TextDocumentIdentifier struct {
	Uri string `json:"uri"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type Location struct {
	Uri   string `json:"uri"`
	Range Range  `json:"range"`
}

// TODO: this implementation work so well that it should be propagated to the other 'Params'
type DefinitionParams struct {
	TextDocumentPositionParams
}

type DefinitionResults struct {
	Location
}

func ProcessGoToDefinition(data []byte, openFiles map[string]*checker.FileDefinition, rawFiles map[string][]byte) (response []byte, fileName string) {
	var req RequestMessage[DefinitionParams]

	err := json.Unmarshal(data, &req)
	if err != nil {
		slog.Warn("error while decoding/unmarshalling lsp client data, " + err.Error())
		return nil, ""
	}

	position := lexer.Position{
		Line:      int(req.Params.Position.Line),
		Character: int(req.Params.Position.Character),
	}

	fileUri, err := url.PathUnescape(req.Params.TextDocument.Uri) // needed for windows os
	if err != nil {
		slog.Error("file uri received from client is malformated. "+err.Error(),
			slog.Group("details",
				slog.String("file_uri", req.Params.TextDocument.Uri),
				slog.Any("request_info", req),
			),
		)
		fileUri = req.Params.TextDocument.Uri
	}

	currentFile := openFiles[fileUri]
	if currentFile == nil {
		msg := ("file requested by lsp client for 'go-to-definition' is not open on the server. That file must be open to make any computation")
		slog.Error(msg,
			slog.Group("details",
				slog.String("uri", req.Params.TextDocument.Uri),
				slog.Any("unmarshalled_req", req),
				slog.String("received_req", string(data)),
			),
		)
		panic(msg)
	}

	defer func() {
		if r := recover(); r != nil {
			msg := r.(string)
			slog.Error(msg,
				slog.Group("details",
					slog.String("uri", req.Params.TextDocument.Uri),
					slog.Any("position", position),
					slog.Any("unmarshalled_req", req),
					slog.String("received_req", string(data)),
				),
			)
			panic(msg)
		}
	}()

	fileNames, reaches, errGoTo := gota.GoToDefinition(currentFile, position)

	var res ResponseMessage[[]DefinitionResults]
	res.Id = req.Id
	res.JsonRpc = req.JsonRpc

	for index := range len(fileNames) {
		fileName = fileNames[index]
		targetFileNameURI := fileNames[index]
		reach := reaches[index]

		if targetFileNameURI == "" {
			msg := ("found a symbol definition without a valid fileName during 'go-to-definition'")
			slog.Error(msg,
				slog.Group("details",
					slog.String("fileName", currentFile.FileName()),
					slog.Any("file_def", currentFile),
					slog.Any("file_names_found", fileNames),
					slog.Any("reaches", reaches),
				),
			)
			panic(msg)
		}

		result := DefinitionResults{}
		result.Uri = targetFileNameURI
		result.Range = convertParserRangeToLspRange(reach)

		res.Result = append(res.Result, result)
	}

	if errGoTo != nil {
		res.Result = nil
	}

	data, err = json.Marshal(res)
	if err != nil {
		slog.Warn("error while encoding/marshalling data for lsp client, " + err.Error())
		return nil, fileName
	}

	return data, fileName
}

type FoldingRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type FoldingRangeResult struct {
	StartLine      uint             `json:"startLine"`
	StartCharacter uint             `json:"startCharacter"`
	EndLine        uint             `json:"endLine"`
	EndCharacter   uint             `json:"endCharacter"`
	Kind           FoldingRangeKind `json:"kind"`
}

type FoldingRangeKind string

const (
	foldingRangeComment FoldingRangeKind = "comment"
	foldingRangeImport  FoldingRangeKind = "imports"
	foldingRangeRegion  FoldingRangeKind = "region"
)

// The first folding request is not garantied to succeed because if the first the client
// send the first folding request before all initial project wide diagnostics are done,
// then 'storage' will not have all files content
func ProcessFoldingRangeRequest(data []byte, storage *WorkSpaceStore, textFromClient map[string][]byte, muTextFromClient *sync.Mutex) (response []byte, fileName string) {
	req := RequestMessage[FoldingRangeParams]{}

	err := json.Unmarshal(data, &req)
	if err != nil {
		slog.Warn("error while decoding/unmarshalling lsp client data, " + err.Error())
		return nil, ""
	}

	fileUri, err := url.PathUnescape(req.Params.TextDocument.Uri)
	if err != nil {
		slog.Error("file uri received from client is malformated. "+err.Error(),
			slog.Group("details",
				slog.String("file_uri", req.Params.TextDocument.Uri),
				slog.Any("request_info", req),
			),
		)
		fileUri = req.Params.TextDocument.Uri
	}

	rootNode := getParseTreeForExistingFile(fileUri, storage, textFromClient, muTextFromClient)

	defer func() {
		if r := recover(); r != nil {
			msg := r.(string)
			slog.Error(msg,
				slog.Group("details",
					slog.String("file_uri", fileUri),
					slog.Any("unmarshalled_req", req),
					slog.String("received_req", string(data)),
					slog.String("file_content", string(storage.RawFiles[fileUri])),
					slog.Any("root_node", rootNode),
				),
			)
			panic(msg)
		}
	}()

	groups, comments := gota.FoldingRange(rootNode)

	var res ResponseMessage[[]FoldingRangeResult]
	res.Id = req.Id
	res.JsonRpc = req.JsonRpc

	for _, group := range groups {
		groupRange := group.Range()
		reach := convertParserRangeToLspRange(groupRange)

		if reach.Start.Line != reach.End.Line { // end_line > start_line
			reach.End.Line--
		}

		fold := FoldingRangeResult{
			StartLine:      reach.Start.Line,
			StartCharacter: reach.Start.Character,
			EndLine:        reach.End.Line,
			EndCharacter:   reach.End.Character,
			Kind:           foldingRangeRegion,
		}

		res.Result = append(res.Result, fold)
	}

	for _, comment := range comments {
		commentRange := comment.Range()
		reach := convertParserRangeToLspRange(commentRange)

		fold := FoldingRangeResult{
			StartLine:      reach.Start.Line,
			StartCharacter: reach.Start.Character,
			EndLine:        reach.End.Line,
			EndCharacter:   reach.End.Character,
			Kind:           foldingRangeComment,
		}

		if comment.GoCode != nil {
			fold.Kind = foldingRangeImport
		}

		res.Result = append(res.Result, fold)
	}

	responseData, err := json.Marshal(res)
	if err != nil {
		slog.Warn("error while encoding/marshalling data for lsp client, " + err.Error())
		return nil, fileName
	}

	return responseData, fileName
}

func getParseTreeForExistingFile(uri string, storage *WorkSpaceStore, textFromClient map[string][]byte, muTextFromClient *sync.Mutex) *parser.GroupStatementNode {
	var rootNode *parser.GroupStatementNode = nil

	muTextFromClient.Lock()
	defer muTextFromClient.Unlock()

	fileContent, ok := textFromClient[uri]
	if ok {
		rootNode, _ = gota.ParseSingleFile(fileContent)
		return rootNode
	}

	rootNode, ok = storage.ParsedFiles[uri]
	if ok {
		return rootNode
	}

	// fallback
	fileContent, ok = storage.RawFiles[uri]
	if ok {
		rootNode, _ = gota.ParseSingleFile(fileContent)
		return rootNode
	}

	return nil
}
