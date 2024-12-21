package main

import (
	"log"
	"os"
	"strings"
	"sync"

	"encoding/json"
	"net/url"

	"go-template-lsp/lsp"

	"github.com/yayolande/gota"
	"github.com/yayolande/gota/parser"
	"github.com/yayolande/gota/lexer"
	checker "github.com/yayolande/gota/analyzer"
)

type workSpaceStore struct {
	rawFiles				map[string][]byte
	parsedFiles			map[string]*parser.GroupStatementNode
	openFilesAnalyzed	map[string]*checker.FileDefinition
	openedFilesError	map[string][]lexer.Error
}

func main() {

	// str := "Content-Length: 865\r\n\r\n" + `{"method":"initialize","jsonrpc":"2.0","id":1,"params":{"workspaceFolders":null,"capabilities":{"textDocument":{"completion":{"dynamicRegistration":false,"completionList":{"itemDefaults":["commitCharacters","editRange","insertTextFormat","insertTextMode","data"]},"contextSupport":true,"completionItem":{"snippetSupport":true,"labelDetailsSupport":true,"insertTextModeSupport":{"valueSet":[1,2]},"resolveSupport":{"properties":["documentation","detail","additionalTextEdits","sortText","filterText","insertText","textEdit","insertTextFormat","insertTextMode"]},"insertReplaceSupport":true,"tagSupport":{"valueSet":[1]},"preselectSupport":true,"deprecatedSupport":true,"commitCharactersSupport":true},"insertTextMode":1}}},"rootUri":null,"rootPath":null,"clientInfo":{"version":"0.10.1+v0.10.1","name":"Neovim"},"processId":230750,"workDoneToken":"1","trace":"off"}}`

	// scanner := bufio.NewScanner(strings.NewReader(str))
	// scanner := bufio.NewScanner(os.Stdin)
	// scanner.Split(inputParsingSplitFunc)
	configureLogging()

	// scanner := lsp.Decode(strings.NewReader(str))
	scanner := lsp.ReceiveInput(os.Stdin)

	// ******************************************************************************
	// WARNING: In under no cirscumstance the 4 variable below should be re-assnigned
	// Otherwise, a nasty bug will appear (value not synced with the rest of the app)
	// ******************************************************************************

	storage := &workSpaceStore{}

	rootPathNotication := make(chan string, 2)
	textChangedNotification := make(chan bool, 2)
	textFromClient := make(map[string][]byte)
	muTextFromClient := new(sync.Mutex)

	go ProcessDiagnosticNotification(storage, rootPathNotication, textChangedNotification, textFromClient, muTextFromClient)

	var request lsp.RequestMessage[any]
	var response []byte
	var isRequestResponse bool	// Response: true <====> Notification: false
	var fileURI string
	var fileContent []byte

	// TODO: What if 'initialize' request is not sent as first request ?
	// Check LSP documentation to learn how to handle those issues
	// https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#initialize

	// TODO: what if the file to process is out of the project ? For instance what happen if client request diagnostic
	// for a file outside of the project ?
	for scanner.Scan() {
		data := scanner.Bytes()

		// TODO: All over the code base, replace 'json' module by a custom made 'stringer' tool
		// because it is more performat
		json.Unmarshal(data, &request)
		// log.Printf("Received json struct: %q\n", data)

		log.Printf("Received json struct: %+v\n", request)

		// TODO: behavior of the 'method' do not respect the LSP spec. For instance 'initialize' must only happen once
		// However there is nothing stoping a rogue program to 'initialize' more than once, or even to not 'initialize' at all
		switch request.Method {
		case "initialize":
			var rootURI string
			response, rootURI = lsp.ProcessInitializeRequest(data)

			notifyTheRootPath(rootPathNotication, rootURI)
			rootPathNotication = nil

			isRequestResponse = true
		case "initialized":
			isRequestResponse = false
			lsp.ProcessInitializedNotificatoin(data)
		case "textDocument/didOpen":
			isRequestResponse = false
			fileURI, fileContent = lsp.ProcessDidOpenTextDocumentNotification(data)

			insertTextDocumentToDiagnostic(fileURI, fileContent, textChangedNotification, textFromClient, muTextFromClient)
		case "textDocument/didChange":
			isRequestResponse = false
			fileURI, fileContent = lsp.ProcessDidChangeTextDocumentNotification(data)

			insertTextDocumentToDiagnostic(fileURI, fileContent, textChangedNotification, textFromClient, muTextFromClient)
		case "textDocument/didClose":
			// TODO: Not sure what to do
		case "textDocument/hover":
			isRequestResponse = true
			response = lsp.ProcessHoverRequest(data)
		case "textDocument/definition":
			isRequestResponse = true
			response, fileURI, fileContent = lsp.ProcessGoToDefinition(data, storage.openFilesAnalyzed, storage.rawFiles)

			insertTextDocumentToDiagnostic(fileURI, fileContent, textChangedNotification, textFromClient, muTextFromClient)
		}

		if isRequestResponse {
			lsp.SendToLspClient(os.Stdout, response)
			/*
			response = lsp.Encode(response)
			lsp.SendOutput(os.Stdout, response)
			*/
		}

		// log.Printf("Sent json struct: %+v", string(response))

		response = nil
		isRequestResponse = false
	}

	if scanner.Err() != nil {
		log.Printf("error: ", scanner.Err().Error())
	}

	log.Printf("\n Shutting down custom lsp server")
}

// Queue like system that notify concerned goroutine when new 'text document' is received from the client.
// Not all sent 'text document' are processed in order, or even processed at all. 
// In other word, if the same document is inserted many time, only the most recent will be processed when 
// concerned goroutine is ready to do so
func insertTextDocumentToDiagnostic(uri string, content []byte, textChangedNotification chan bool, textFromClient map[string][]byte, muTextFromClient *sync.Mutex) {
	if content == nil || uri == "" {
		return
	}

	muTextFromClient.Lock()
	textFromClient[uri] = content
	muTextFromClient.Unlock()

	if len(textChangedNotification) == 0 {
		textChangedNotification <- true
	}

	if len(textChangedNotification) >= 2 {
		panic("'textChangedNotification' channel size should never exceed 1, otherwise goroutine might be blocked and nasty bug may appear. " +
			"as per standard, when there is at least one 'text' from client waiting to be processed, len(textChangedNotification) must remain at 1")
	}
}

func notifyTheRootPath(rootPathNotication chan string, rootURI string) {
	if rootPathNotication == nil {
		panic("unexpected usage of 'rootPathNotication' channel. This channel should be used only once to send root path. " + 
			"Either it hasn't been initialized at least once, or it has been used more than once (bc. channel set to nil after first use)")
	}

	if cap(rootPathNotication) == 1 {
		panic("'rootPathNotication' channel should be empty at this point. " +
			"Either an element have been illegally inserted or the 'initialize' method might be the responsible")
	}

	if cap(rootPathNotication) == 1 {
		panic("'rootPathNotication' channel should have a buffer capacity of at least 2, to have its blocking behavior")
	}

	rootPathNotication <- rootURI

	close(rootPathNotication)
}

// Independently diagnostic code source and send notifications to client
func ProcessDiagnosticNotification(storage *workSpaceStore, rootPathNotication chan string, textChangedNotification chan bool, textFromClient map[string][]byte, muTextFromClient *sync.Mutex) {
	if rootPathNotication == nil || textChangedNotification == nil {
		panic("channel(s) for 'ProcessDiagnosticNotification()' not properly initialized")
	}

	if textFromClient == nil {
		panic("empty reference to 'textFromClient'. LSP server won't be able to handle text update from client")
	}

	var rootPath string
	var targetExtension string

	rootPath, ok := <- rootPathNotication
	rootPathNotication = nil
	if !ok {
		panic("rootPathNotification is closed or nil within 'ProcessDiagnosticNotification()'. " + 
			"that channel should only emit the root path once, and then be closed right after and then never used again")
	}

	rootPath = uriToFilePath(rootPath)
	targetExtension = ".html"
	// TODO: When I use the value below, I get nasty error (panic). Investigate it later on
	// targetExtension = ".tmpl"
	storage.rawFiles = gota.OpenProjectFiles(rootPath, targetExtension)

	// Since the client only recognize URI, it is better to adopt this early 
	// on the server as well to avoid perpetual conversion from 'uri' to 'path'
	storage.rawFiles = moveKeysFromFilePathToUri(storage.rawFiles)

	// TODO: should I check: len(rawFiles) == len(storage.parsedFiles)
	storage.parsedFiles, _ = gota.ParseFilesInWorkspace(storage.rawFiles)
	if storage.parsedFiles == nil {
		storage.parsedFiles = make(map[string]*parser.GroupStatementNode)
	}

	// TODO: For improved perf. also fetch gota.getWorkspaceTemplateDefinition()
	// and only recompute it when a specific file change

	notification := &lsp.NotificationMessage[lsp.PublishDiagnosticsParams]{
		JsonRpc: "2.0",
		Method: "textDocument/publishDiagnostics",
		Params: lsp.PublishDiagnosticsParams{
			Uri: "place_holder_by_lsp_server--should_never_reach_the_client",
			Diagnostics: []lsp.Diagnostic{},
		},
	}

	// watch for client edit notification (didChange, ...)
	storage.openFilesAnalyzed = make(map[string]*checker.FileDefinition)
	storage.openedFilesError = make(map[string][]gota.Error)
	cloneTextFromClient := make(map[string][]byte)

	for _ = range textChangedNotification {
		if len(textFromClient) == 0 {
			panic("got a change notification but the text from client was empty. " + 
				"check that the 'textFromClient' still point to the correct address " + 
				"or that the notification wasn't fired by accident") 
		}

		// TODO: handle the case where file is not in workspaceFolders
		// the lsp should be still working, but it should not use data for the other workspace


		muTextFromClient.Lock()

		for uri, fileContent := range textFromClient {
			cloneTextFromClient[uri] = fileContent
			delete(textFromClient, uri)
		}

		muTextFromClient.Unlock()

		for uri, fileContent := range cloneTextFromClient {
			// TODO; this code below will one day cause trouble
			// In fact, if a file opened by the lsp client is not in the root or haven't the mandatory exntension
			// then the condition after the loop below will fail and launch a panic
			// if len(cloneTextFromClient) != 0 
			if ! isFileInsideWorkspace(uri, rootPath, targetExtension) {
				log.Printf("oups ... this file is not considerated part of the project ::: file = %s\n", uri)
				continue
			}

			storage.rawFiles[uri] = fileContent
			parseTree, localErrs := gota.ParseSingleFile(fileContent)

			storage.parsedFiles[uri] = parseTree
			storage.openedFilesError[uri] = localErrs

			// delete(cloneTextFromClient, uri)
		}

		for uri, _ := range storage.openedFilesError {
			file, localErrs := gota.DefinitionAnalysisSingleFile(uri, storage.parsedFiles)

			storage.openFilesAnalyzed[uri] = file
			storage.openedFilesError[uri] = append(storage.openedFilesError[uri], localErrs...)
		}

		var errs []gota.Error
		for uri, _ := range cloneTextFromClient {
			// file, localErrs := gota.DefinitionAnalysisSingleFile(uri, storage.parsedFiles)

			// storage.openFilesAnalyzed[uri] = file

			// storage.openedFilesError[uri] = append(storage.openedFilesError[uri], localErrs...)
			errs = storage.openedFilesError[uri]

			notification = clearPushDiagnosticNotification(notification)
			notification = setParseErrosToDiagnosticsNotification(errs, notification)
			notification.Params.Uri = uri

			response, err := json.Marshal(notification)
			if err != nil {
				log.Printf("failed marshalling for notification response. \n notification = %#v \n", notification)
				panic("Diagnostic Handler is Unable to 'marshall' notification response, " + err.Error())
			}

			lsp.SendToLspClient(os.Stdout, response)
			// log.Printf("sent diagnostic notification: \n %q", response)
			// log.Printf("\n\n simpler notif: \n %#v \n", notification)
			log.Printf("\n\nmessage sent to client :: msg = %#v\n\n", notification)
		}

		clear(cloneTextFromClient)

		// clear(textFromClient)
	}
}

func isFileInsideWorkspace(uri string, rootPath string, allowedFileExntesion string) bool {
	path := uri
	rootPath = filePathToUri(rootPath)

	if ! strings.HasPrefix(path, rootPath) {
		return false
	}

	if ! strings.HasSuffix(path, allowedFileExntesion) {
		return false
	}

	return true
}

func clearPushDiagnosticNotification(notification *lsp.NotificationMessage[lsp.PublishDiagnosticsParams]) *lsp.NotificationMessage[lsp.PublishDiagnosticsParams] {
	notification.Params.Diagnostics = []lsp.Diagnostic{}
	notification.Params.Uri = ""

	return notification
}

func setParseErrosToDiagnosticsNotification(errs []gota.Error, response *lsp.NotificationMessage[lsp.PublishDiagnosticsParams]) *lsp.NotificationMessage[lsp.PublishDiagnosticsParams] {
	if response == nil {
		panic("diagnostics errors cannot be appended on 'nil' response. first create the the response")
	}

	response.Params.Diagnostics = []lsp.Diagnostic{}

	for _, err := range errs {
		if err == nil {
			panic("'nil' should not be in the error list. if you to represent an absence of error in the list, just dont insert it")
		}

		diagnostic := lsp.Diagnostic{
			Message: err.GetError(),
			Range: *fromParserRangeToLspRange(err.GetRange()),
		}

		response.Params.Diagnostics = append(response.Params.Diagnostics, diagnostic)
	}

	return response
}

func fromParserRangeToLspRange(rg lexer.Range) *lsp.Range {
	reach := &lsp.Range{
		Start: lsp.Position{
			Line: uint(rg.Start.Line),
			Character: uint(rg.Start.Character),
		},
		End: lsp.Position{
			Line: uint(rg.End.Line),
			Character: uint(rg.End.Character),
		},
	}

	return reach
}

// TODO: make this function work for windows path as well
// Undefined behavior when the windows path use special encoding for colon(":")
// Additionally, special character are not always well escaped between client and server
func uriToFilePath(uri string) string {
	if uri == "" {
		panic("URI to a file cannot be empty")
	}

	u, err := url.Parse(uri)
	if err != nil {
		panic("unable to convert from URI to os path, " + err.Error())
	}

	if u.Scheme == "" {
		panic("expected a scheme for the file's 'URI' but found nothing. uri = " + uri)
	}

	if u.Scheme != "file" {
		panic("cannot handle any scheme other than 'file'. uri = " + uri)
	}

	if u.RawQuery != "" {
		panic("'?' character is not permited within a file's 'URI'. uri = " + uri)
	}

	if u.Fragment != "" {
		panic("'#' character is not permited within a file's 'URI'. uri = " + uri)
	}

	if u.Path == "" {
		panic("path to a file cannot be empty (conversion from uri to path)")
	}

	return u.Path
}

// TODO: make this function work for windows path as well
// Undefined behavior when the windows path use special encoding for colon(":")
// Additionally, special character are not always well escaped between client and server
func filePathToUri(path string) string {
	if path == "" {
		panic("path to a file cannot be empty")
	}

	u, err := url.Parse(path)
	if err != nil {
		panic("unable to convert from os path to URI, " + err.Error())
	}

	if u.Scheme != "" {
		panic("expected empty scheme for the file's 'URI' but found something. uri = ," + path)
	}

	if u.RawQuery != "" {
		panic("'?' character is not permited within a file's 'URI'. uri = " + path)
	}

	if u.Fragment != "" {
		panic("'#' character is not permited within a file's 'URI'. uri = " + path)
	}

	u.Scheme = "file"

	return u.String()
}

func moveKeysFromFilePathToUri(files map[string][]byte) map[string][]byte {
	if len(files) == 0 {
		return files
	}

	var uri string
	filesWithUriKeys := make(map[string][]byte)

	for path, fileContent := range files {
		uri = filePathToUri(path)
		filesWithUriKeys[uri] = fileContent
	}

	return filesWithUriKeys
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


