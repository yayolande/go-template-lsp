package main

import (
	"log"
	"maps"
	"os"
	"strings"
	"sync"

	"encoding/json"
	"net/url"

	"github.com/yayolande/go-template-lsp/lsp"

	"github.com/yayolande/gota"
	checker "github.com/yayolande/gota/analyzer"
	"github.com/yayolande/gota/lexer"
	"github.com/yayolande/gota/parser"
)

type workSpaceStore struct {
	rawFiles          map[string][]byte
	parsedFiles       map[string]*parser.GroupStatementNode
	ErrorsParsedFiles map[string][]lexer.Error

	openedFilesAnalyzed map[string]*checker.FileDefinition
	ErrorsAnalyzedFiles map[string][]lexer.Error
}

func main() {

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
	var isRequestResponse bool // Response: true <====> Notification: false
	var isExiting bool = false
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

		if isExiting {
			if request.Method == "exit" {
				break
			} else {
				response = lsp.ProcessIllegalRequestAfterShutdown(request.JsonRpc, request.Id)
				lsp.SendToLspClient(os.Stdout, response)
			}

			continue
		}

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
		case "shutdown":
			// TODO: close opened buffers and stop task analysis
			isExiting = true
			isRequestResponse = true
			response = lsp.ProcessShutdownRequest(request.JsonRpc, request.Id)

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
			response = lsp.ProcessHoverRequest(data, storage.openedFilesAnalyzed)
		case "textDocument/definition":
			isRequestResponse = true
			response, _ = lsp.ProcessGoToDefinition(data, storage.openedFilesAnalyzed, storage.rawFiles)

			// insertTextDocumentToDiagnostic(fileURI, fileContent, textChangedNotification, textFromClient, muTextFromClient)
		}

		if isRequestResponse {
			lsp.SendToLspClient(os.Stdout, response)
		}

		response = nil
		isRequestResponse = false
	}

	if scanner.Err() != nil {
		log.Printf("error while closing LSP: ", scanner.Err().Error())
	}

	log.Printf("\n Shutting down Go Template LSP server")
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

	rootPath, ok := <-rootPathNotication
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
	storage.rawFiles = convertKeysFromFilePathToUri(storage.rawFiles)

	muTextFromClient.Lock()

	maps.Copy(textFromClient, storage.rawFiles)

	if len(textChangedNotification) == 0 { // Trigger analysis
		textChangedNotification <- true
	}

	muTextFromClient.Unlock()

	storage.parsedFiles = make(map[string]*parser.GroupStatementNode)
	storage.openedFilesAnalyzed = make(map[string]*checker.FileDefinition)

	storage.ErrorsAnalyzedFiles = make(map[string][]lexer.Error)
	storage.ErrorsParsedFiles = make(map[string][]lexer.Error)

	notification := &lsp.NotificationMessage[lsp.PublishDiagnosticsParams]{
		JsonRpc: "2.0",
		Method:  "textDocument/publishDiagnostics",
		Params: lsp.PublishDiagnosticsParams{
			Uri:         "place_holder_by_lsp_server--should_never_reach_the_client",
			Diagnostics: []lsp.Diagnostic{},
		},
	}

	// watch for client edit notification (didChange, ...)
	var chainedFiles []gota.FileAnalysisAndError = nil
	cloneTextFromClient := make(map[string][]byte)

	for _ = range textChangedNotification {
		log.Printf("==> lsp before compute:\n size all files opened = %d\n size 'textFromClient' = %d\n", len(storage.openedFilesAnalyzed), len(textFromClient))
		log.Printf("==> lsp before 1:\n size errors all files opened = %d\n size 'textFromClient' = %d\n", len(storage.ErrorsAnalyzedFiles), len(textFromClient))

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
		}

		clear(textFromClient)

		muTextFromClient.Unlock()

		namesOfFileChanged := make([]string, 0, len(cloneTextFromClient))

		for uri, fileContent := range cloneTextFromClient {
			if !isFileInsideWorkspace(uri, rootPath, targetExtension) {
				log.Printf("oups ... this file is not considerated part of the project ::: file = %s\n", uri)
				continue
			}

			storage.rawFiles[uri] = fileContent
			parseTree, localErrs := gota.ParseSingleFile(fileContent)

			if parseTree == nil {
				log.Printf("oups ... found an empty parseTree(file)\n fileName = `%s`\n fileContent = `%s`\n", uri, fileContent)
				continue
			}

			storage.parsedFiles[uri] = parseTree
			storage.ErrorsParsedFiles[uri] = localErrs

			namesOfFileChanged = append(namesOfFileChanged, uri)
		}

		chainedFiles = nil

		if len(cloneTextFromClient) == len(storage.parsedFiles) {
			chainedFiles = gota.DefinitionAnalisisWithinWorkspace(storage.parsedFiles)

		} else if len(cloneTextFromClient) > 0 {
			chainedFiles = gota.DefinitionAnalysisChainTrigerredByBatchFileChange(storage.parsedFiles, namesOfFileChanged...)

			// } else if len(cloneTextFromClient) == 1 {
			// chainedFiles = gota.DefinitionAnalysisChainTrigerredBysingleFileChange(namesOfFileChanged[0], storage.parsedFiles)
		}

		namesOfFileChanged = namesOfFileChanged[:0] // empty the slice

		// for uri, _ := range cloneTextFromClient {
		//chainedFiles := gota.DefinitionAnalysisChainTrigerredBysingleFileChange(uri, storage.parsedFiles)

		log.Println("\n===== list of affected files =====\n")

		for _, fileAnalyzed := range chainedFiles {
			localUri := fileAnalyzed.FileName

			log.Printf("<----> fileName affected = %s\n", localUri)

			storage.openedFilesAnalyzed[localUri] = fileAnalyzed.File
			storage.ErrorsAnalyzedFiles[localUri] = fileAnalyzed.Errs
		}

		for uri := range storage.openedFilesAnalyzed {
			errs := make([]gota.Error, 0, len(storage.ErrorsParsedFiles[uri])+len(storage.ErrorsAnalyzedFiles[uri]))

			errs = append(errs, storage.ErrorsParsedFiles[uri]...)
			errs = append(errs, storage.ErrorsAnalyzedFiles[uri]...)

			notification = clearPushDiagnosticNotification(notification)
			notification = setParseErrosToDiagnosticsNotification(errs, notification)
			notification.Params.Uri = uri

			response, err := json.Marshal(notification)
			if err != nil {
				log.Printf("failed marshalling for notification response. \n notification = %#v \n", notification)
				panic("Diagnostic Handler is Unable to 'marshall' notification response, " + err.Error())
			}

			lsp.SendToLspClient(os.Stdout, response)

			// log.Printf("\n\nmessage sent to client :: msg = %#v\n\n", notification)
		}
		// }

		clear(cloneTextFromClient)

		if len(storage.openedFilesAnalyzed) != len(storage.parsedFiles) {
			log.Printf("size mismatch between 'semantic analysed files' and 'parsed files'\n size analysed files = %d\n size parsed files = %d\n", len(storage.openedFilesAnalyzed), len(storage.parsedFiles))
			panic("size mismatch between 'semantic analysed files' and 'parsed files'")
		} else if len(storage.openedFilesAnalyzed) != len(storage.rawFiles) {
			log.Printf("size mismatch between 'semantic analysed files' and 'raw files'\n size analysed files = %d\n size raw files = %d\n", len(storage.openedFilesAnalyzed), len(storage.rawFiles))
			panic("size mismatch between 'semantic analysed files' and 'raw files'")
		} else if len(storage.ErrorsAnalyzedFiles) != len(storage.ErrorsParsedFiles) {
			log.Printf("size mismatch between 'errors semantic analysed files' and 'errors parsed files'\n size analysed files = %d\n size raw files = %d\n", len(storage.ErrorsAnalyzedFiles), len(storage.ErrorsParsedFiles))
			panic("size mismatch between 'errors semantic analysed files' and 'errors parsed files'")
		} else if len(storage.ErrorsAnalyzedFiles) != len(storage.openedFilesAnalyzed) {
			log.Printf("size mismatch between errors associated to files and opened files", len(storage.ErrorsAnalyzedFiles), len(storage.openedFilesAnalyzed))
			panic("size mismatch between errors associated to files and opened files")
		}
	}
}

func isFileInsideWorkspace(uri string, rootPath string, allowedFileExntesion string) bool {
	path := uri
	rootPath = filePathToUri(rootPath)

	if !strings.HasPrefix(path, rootPath) {
		return false
	}

	if !strings.HasSuffix(path, allowedFileExntesion) {
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
			Range:   *fromParserRangeToLspRange(err.GetRange()),
		}

		response.Params.Diagnostics = append(response.Params.Diagnostics, diagnostic)
	}

	return response
}

func fromParserRangeToLspRange(rg lexer.Range) *lsp.Range {
	reach := &lsp.Range{
		Start: lsp.Position{
			Line:      uint(rg.Start.Line),
			Character: uint(rg.Start.Character),
		},
		End: lsp.Position{
			Line:      uint(rg.End.Line),
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

func convertKeysFromFilePathToUri(files map[string][]byte) map[string][]byte {
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
