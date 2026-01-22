package main

import (
	"flag"
	"fmt"

	"log/slog"
	"maps"
	"os"
	"strings"
	"sync"

	"encoding/json"
	"net/url"
	"path/filepath"
	"runtime"

	"github.com/yayolande/go-template-lsp/lsp"

	"github.com/yayolande/gota"
	checker "github.com/yayolande/gota/analyzer"
	"github.com/yayolande/gota/lexer"
	"github.com/yayolande/gota/parser"
)

type workSpaceStore = lsp.WorkSpaceStore

/*
type workSpaceStore struct {
	RootPath          string
	RawFiles          map[string][]byte
	ParsedFiles       map[string]*parser.GroupStatementNode
	ErrorsParsedFiles map[string][]lexer.Error

	OpenedFilesAnalyzed map[string]*checker.FileDefinition
	ErrorsAnalyzedFiles map[string][]lexer.Error
}
*/

type requestCounter struct {
	Initialize   int
	Initialized  int
	Shutdown     int
	TextDocument struct {
		DidClose  int
		DidOpen   int
		DidChange int
	}
	FoldingRange int
	Definition   int
	Hover        int
	Other        int
}

var TARGET_FILE_EXTENSIONS []string = []string{
	"go.html", "go.tmpl", "go.txt",
	"gohtml", "gotmpl", "tmpl", "tpl",
	"html",
}
var SERVER_NAME string = "Go Template LSP"
var SERVER_VERSION string = "0.3.10"
var SERVER_BUILD_DATE string = "2026/01/22 18:00"
var serverCounter requestCounter = requestCounter{}

func main() {
	// 1. Parse CLI arguments
	isversionFlagEnabled := flag.Bool("version", false, "print the LSP version")
	flag.Parse()

	if *isversionFlagEnabled { // print LSP version then exit
		fmt.Printf("%s -- version %s  %s\n", SERVER_NAME, SERVER_VERSION, SERVER_BUILD_DATE)
		os.Exit(0)
	}

	// 2. Start LSP
	configureLogging()
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

	slog.Info("starting lsp server",
		slog.String("server_name", SERVER_NAME),
		slog.String("server_version", SERVER_VERSION),
	)
	defer slog.Info("shutting down lsp server", GetServerGroupLogging(storage, serverCounter, request, textFromClient))

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
		slog.Info("request "+request.Method, GetServerGroupLogging(storage, serverCounter, request, textFromClient))

		switch request.Method {
		case "initialize":
			serverCounter.Initialize++
			var rootURI string
			response, rootURI = lsp.ProcessInitializeRequest(data, SERVER_NAME, SERVER_VERSION)

			notifyTheRootPath(rootPathNotication, rootURI)
			rootPathNotication = nil
			isRequestResponse = true

		case "initialized":
			serverCounter.Initialized++
			isRequestResponse = false
			lsp.ProcessInitializedNotificatoin(data)
		case "shutdown":
			// TODO: close opened buffers and stop task analysis
			serverCounter.Shutdown++
			isExiting = true
			isRequestResponse = true
			response = lsp.ProcessShutdownRequest(request.JsonRpc, request.Id)

		case "textDocument/didOpen":
			serverCounter.TextDocument.DidOpen++
			isRequestResponse = false
			fileURI, fileContent = lsp.ProcessDidOpenTextDocumentNotification(data)

			insertTextDocumentToDiagnostic(fileURI, fileContent, textChangedNotification, textFromClient, muTextFromClient)
		case "textDocument/didChange":
			serverCounter.TextDocument.DidChange++
			isRequestResponse = false
			fileURI, fileContent = lsp.ProcessDidChangeTextDocumentNotification(data)

			insertTextDocumentToDiagnostic(fileURI, fileContent, textChangedNotification, textFromClient, muTextFromClient)
		case "textDocument/didClose":
			serverCounter.TextDocument.DidClose++
			// TODO: Not sure what to do
		case "textDocument/hover":
			serverCounter.Hover++
			isRequestResponse = true
			response = lsp.ProcessHoverRequest(data, storage.OpenedFilesAnalyzed)
		case "textDocument/definition":
			serverCounter.Definition++
			isRequestResponse = true
			response, _ = lsp.ProcessGoToDefinition(data, storage.OpenedFilesAnalyzed, storage.RawFiles)

			// insertTextDocumentToDiagnostic(fileURI, fileContent, textChangedNotification, textFromClient, muTextFromClient)
		case "textDocument/foldingRange":
			serverCounter.FoldingRange++
			isRequestResponse = true
			response, _ = lsp.ProcessFoldingRangeRequest(data, storage, textFromClient, muTextFromClient)
		default:
			serverCounter.Other++
		}

		if isRequestResponse {
			lsp.SendToLspClient(os.Stdout, response)

			// INFO: This is only for debug purpose
			res := lsp.ResponseMessage[any]{}
			_ = json.Unmarshal(response, &res)
			slog.Info("response "+request.Method,
				slog.Group("server",
					slog.String("name", SERVER_NAME),
					slog.String("version", SERVER_VERSION),
					slog.String("root_path", storage.RootPath),
					slog.Any("request_counter", serverCounter),
					slog.Any("open_files", mapToKeys(storage.RawFiles)),
					slog.Any("files_waiting_processing", mapToKeys(textFromClient)),
					slog.Any("last_response", res),
				),
			)
		}

		response = nil
		isRequestResponse = false
	}

	if scanner.Err() != nil {
		msg := "error while closing LSP: " + scanner.Err().Error()
		slog.Error(msg)
		panic(msg)
	}
}

// Queue like system that notify concerned goroutine when new 'text document' is received from the client.
// Not all sent 'text document' are processed in order, or even processed at all.
// In other word, if the same document is inserted many time, only the most recent will be processed when
// concerned goroutine is ready to do so
func insertTextDocumentToDiagnostic(uri string, content []byte, textChangedNotification chan bool, textFromClient map[string][]byte, muTextFromClient *sync.Mutex) {
	if uri == "" {
		return
	}

	muTextFromClient.Lock()
	textFromClient[uri] = content

	if len(textChangedNotification) == 0 {
		textChangedNotification <- true
	}

	muTextFromClient.Unlock()

	if len(textChangedNotification) >= 2 {
		msg := ("'textChangedNotification' channel size should never exceed 1, otherwise goroutine might be blocked and nasty bug may appear. " +
			"as per standard, when there is at least one 'text' from client waiting to be processed, len(textChangedNotification) must remain at 1")
		slog.Error("msg",
			slog.Group("error_details",
				slog.String("uri_file_to_diagnostic", uri),
				slog.String("content_file_to_diagnostic", string(content)),
				slog.Any("files_waiting_processing", mapToKeys(textFromClient)),
			),
		)
		panic(msg)
	}
}

func notifyTheRootPath(rootPathNotication chan string, rootURI string) {
	if rootPathNotication == nil {
		return // do nothing, most likely the root path has already initialized
	}

	if len(rootPathNotication) > 0 {
		msg := ("'rootPathNotication' channel should be empty at this point. " +
			"Either an element have been illegally inserted or the 'initialize' method might be responsible" +
			"rootURI = " + rootURI)
		slog.Error(msg)
		panic(msg)

	} else if cap(rootPathNotication) < 2 {
		msg := ("'rootPathNotication' channel should have a buffer capacity of at least 2, to have its blocking behavior")
		slog.Error(msg)
		panic(msg)
	}

	rootPathNotication <- rootURI
	close(rootPathNotication)
}

// Independently diagnostic code source and send notifications to client
func ProcessDiagnosticNotification(storage *workSpaceStore, rootPathNotication chan string, textChangedNotification chan bool, textFromClient map[string][]byte, muTextFromClient *sync.Mutex) {
	if rootPathNotication == nil || textChangedNotification == nil {
		msg := ("channel(s) for 'ProcessDiagnosticNotification()' not properly initialized")
		slog.Error(msg)
		panic(msg)
	}

	if textFromClient == nil {
		msg := ("empty reference to 'textFromClient'. LSP server won't be able to handle text update from client")
		slog.Error(msg)
		panic(msg)
	}

	rootPath, ok := <-rootPathNotication
	rootPathNotication = nil
	if !ok {
		msg := ("rootPathNotification is closed or nil within 'ProcessDiagnosticNotification()'. " +
			"that channel should only emit the root path once, and then be closed right after and then never used again")
		slog.Error(msg)
		panic(msg)
	}

	rootPath = uriToFilePath(rootPath)

	storage.RootPath = rootPath
	storage.RawFiles = gota.OpenProjectFiles(rootPath, TARGET_FILE_EXTENSIONS)

	// Since the client only recognize URI, it is better to adopt this early
	// on the server as well to avoid perpetual conversion from 'uri' to 'path'
	storage.RawFiles = convertKeysFromFilePathToUri(storage.RawFiles)

	muTextFromClient.Lock()
	{
		temporaryClone := maps.Clone(textFromClient)
		maps.Copy(textFromClient, storage.RawFiles)
		maps.Copy(textFromClient, temporaryClone)
	}

	if len(textFromClient) > 0 && len(textChangedNotification) == 0 { // Trigger analysis when files found
		textChangedNotification <- true
	}

	muTextFromClient.Unlock()

	storage.ParsedFiles = make(map[string]*parser.GroupStatementNode)
	storage.OpenedFilesAnalyzed = make(map[string]*checker.FileDefinition)
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

	defer func() {
		slog.Info("notif details",
			slog.Any("len_rootPathNotication", len(rootPathNotication)),
			slog.Any("len_textChangedNotification", len(textChangedNotification)),
			slog.Any("open_files", mapToKeys(storage.RawFiles)),
			slog.Any("files_waiting_processing", mapToKeys(textFromClient)),
			slog.Any("storage", storage),
			slog.Any("last_notif", notification),
		)
	}()

	// watch for client edit notification (didChange, ...)
	var chainedFiles []gota.FileAnalysisAndError = nil
	cloneTextFromClient := make(map[string][]byte)

	for _ = range textChangedNotification {
		if len(textFromClient) == 0 {
			msg := ("got a change notification but the text from client was empty. " +
				"check that the 'textFromClient' still point to the correct address " +
				"or that the notification wasn't fired by accident")
			slog.Error(msg)
			panic(msg)
		}

		// This mutex synchronize the 'textFromClient' resource
		// incidently, it also protect/synchronize the shared 'storage.parsedFiles' and 'storage.RawFiles'
		// this property is heavily used within the function 'ProcessFoldingRangeRequest()'
		muTextFromClient.Lock()

		clear(cloneTextFromClient)
		namesOfFileChanged := make([]string, 0, len(textFromClient))

		for uri, fileContent := range textFromClient {
			if !isFileInsideWorkspace(uri, rootPath, TARGET_FILE_EXTENSIONS) {
				slog.Warn("skiped file", slog.String("file_uri", uri))
				continue
			}

			storage.RawFiles[uri] = fileContent // must be done here inbetween mutex
			cloneTextFromClient[uri] = fileContent

			parseTree, localErrs := gota.ParseSingleFile(fileContent) // main processing

			storage.ParsedFiles[uri] = parseTree // must be done here inbetween mutex
			storage.ErrorsParsedFiles[uri] = localErrs
			namesOfFileChanged = append(namesOfFileChanged, uri)
		}

		clear(textFromClient)
		for _ = range len(textChangedNotification) { // clear all notifications
			_ = <-textChangedNotification
		}

		muTextFromClient.Unlock()

		if len(cloneTextFromClient) == 0 {
			continue
		}

		chainedFiles = nil

		// BUG: this is so ugly
		// Semantical analysis of files is tighly coupled with this function
		// I wanted to experiment only with the 'parsing' package,
		// but it is too challenging to remove or comment out the analysis section

		if len(cloneTextFromClient) == len(storage.ParsedFiles) {
			chainedFiles = gota.DefinitionAnalisisWithinWorkspace(storage.ParsedFiles)

		} else if len(cloneTextFromClient) > 0 {
			chainedFiles = gota.DefinitionAnalysisChainTrigerredByBatchFileChange(storage.ParsedFiles, namesOfFileChanged...)

			// } else if len(cloneTextFromClient) == 1 {
			// chainedFiles = gota.DefinitionAnalysisChainTrigerredBysingleFileChange(namesOfFileChanged[0], storage.parsedFiles)
		}

		namesOfFileChanged = namesOfFileChanged[:0] // empty the slice

		for _, fileAnalyzed := range chainedFiles {
			localUri := fileAnalyzed.FileName

			storage.OpenedFilesAnalyzed[localUri] = fileAnalyzed.File
			storage.ErrorsAnalyzedFiles[localUri] = fileAnalyzed.Errs
		}

		for uri := range storage.OpenedFilesAnalyzed {
			errs := make([]gota.Error, 0, len(storage.ErrorsParsedFiles[uri])+len(storage.ErrorsAnalyzedFiles[uri]))

			errs = append(errs, storage.ErrorsParsedFiles[uri]...)
			errs = append(errs, storage.ErrorsAnalyzedFiles[uri]...)

			notification = clearPushDiagnosticNotification(notification)
			notification = setParseErrosToDiagnosticsNotification(errs, notification)
			notification.Params.Uri = uri

			response, err := json.Marshal(notification)
			if err != nil {
				msg := "Diagnostic Handler is Unable to 'marshall' notification response, " + err.Error()
				slog.Error(msg,
					slog.Group("error",
						slog.String("file_uri", uri),
						slog.String("file_content", string(storage.RawFiles[uri])),
						slog.Any("file_parse_error", errs),
						slog.Any("notification", notification),
					),
				)
				panic(msg)
			}

			lsp.SendToLspClient(os.Stdout, response)
		}

		storageSanityCheck(storage)
	}
}

func storageSanityCheck(storage *workSpaceStore) {
	if len(storage.OpenedFilesAnalyzed) != len(storage.ParsedFiles) {
		msg := "size mismatch between 'semantic analysed files' and 'parsed files'"
		slog.Error(msg,
			slog.Group("error_details",
				slog.Int("len_openFilesAnalyzed", len(storage.OpenedFilesAnalyzed)),
				slog.Int("len_parsedFiles", len(storage.ParsedFiles)),
				slog.Any("openedFilesAnalyzed", mapToKeys(storage.OpenedFilesAnalyzed)),
				slog.Any("parsedFiles", mapToKeys(storage.ParsedFiles)),
			),
		)
		panic(msg)

	} else if len(storage.OpenedFilesAnalyzed) != len(storage.RawFiles) {
		msg := "found more 'semantic analysed files' than 'raw files'"
		slog.Error(msg,
			slog.Group("error_details",
				slog.Int("len_openFilesAnalyzed", len(storage.OpenedFilesAnalyzed)),
				slog.Int("len_rawFiles", len(storage.RawFiles)),
				slog.Any("openedFilesAnalyzed", mapToKeys(storage.OpenedFilesAnalyzed)),
				slog.Any("rawFiles", mapToKeys(storage.RawFiles)),
			),
		)
		panic(msg)

	} else if len(storage.ErrorsAnalyzedFiles) != len(storage.ErrorsParsedFiles) {
		msg := "size mismatch between 'errors semantic analysed files' and 'errors parsed files'"
		slog.Error(msg,
			slog.Group("error_details",
				slog.Int("len_errorsAnalyzedFiles", len(storage.ErrorsAnalyzedFiles)),
				slog.Int("len_errorsParsedFiles", len(storage.ErrorsParsedFiles)),
				slog.Any("ErrorsAnalyzedFiles", mapToKeys(storage.ErrorsAnalyzedFiles)),
				slog.Any("ErrorsParsedFiles", mapToKeys(storage.ErrorsParsedFiles)),
			),
		)
		panic(msg)

	} else if len(storage.ErrorsAnalyzedFiles) != len(storage.OpenedFilesAnalyzed) {
		msg := "size mismatch between errors associated to files and opened files"
		slog.Error(msg,
			slog.Group("error_details",
				slog.Int("len_errorsAnalyzedFiles", len(storage.ErrorsAnalyzedFiles)),
				slog.Int("len_openedFilesAnalyzed", len(storage.OpenedFilesAnalyzed)),
				slog.Any("ErrorsAnalyzedFiles", mapToKeys(storage.ErrorsAnalyzedFiles)),
				slog.Any("openedFilesAnalyzed", mapToKeys(storage.OpenedFilesAnalyzed)),
			),
		)
		panic(msg)
	}
}

func isFileInsideWorkspace(uri string, rootPath string, allowedFileExntesions []string) bool {
	path := uri
	rootPath = filePathToUri(rootPath)

	if !strings.HasPrefix(path, rootPath) {
		return false
	}

	return gota.HasFileExtension(path, allowedFileExntesions)
}

func clearPushDiagnosticNotification(notification *lsp.NotificationMessage[lsp.PublishDiagnosticsParams]) *lsp.NotificationMessage[lsp.PublishDiagnosticsParams] {
	notification.Params.Diagnostics = []lsp.Diagnostic{}
	notification.Params.Uri = ""

	return notification
}

func setParseErrosToDiagnosticsNotification(errs []gota.Error, response *lsp.NotificationMessage[lsp.PublishDiagnosticsParams]) *lsp.NotificationMessage[lsp.PublishDiagnosticsParams] {
	if response == nil {
		msg := ("diagnostics errors cannot be appended on 'nil' response. first create the the response")
		slog.Error(msg)
		panic(msg)
	}

	response.Params.Diagnostics = []lsp.Diagnostic{}

	for _, err := range errs {
		if err == nil {
			msg := ("'nil' should not be in the error list. if you to represent an absence of error in the list, just dont insert it")
			slog.Error(msg,
				slog.Group("error_details",
					slog.Any("full_errs", errs),
					slog.Any("partial_response", response),
				),
			)
			panic(msg)
		}

		diagnostic := lsp.Diagnostic{
			Message:  err.GetError(),
			Range:    *fromParserRangeToLspRange(err.GetRange()),
			Severity: 1, // 1 = Error, 2 = Warning, 3 = Info, 4 = Hint
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

func uriToFilePath(uri string) string {
	if uri == "" {
		msg := ("URI to a file cannot be empty")
		slog.Error(msg)
		panic(msg)
	}

	defer func() {
		if err := recover(); err != nil {
			msg, _ := err.(string)
			slog.Error(msg, slog.String("uri_to_convert", uri))
			panic(msg)
		}
	}()

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

	path := u.Path
	if runtime.GOOS == "windows" {
		if path[0] == '/' && len(path) >= 3 && path[2] == ':' {
			path = path[1:]
		}
	}

	path = filepath.FromSlash(path)

	return path
}

func filePathToUri(path string) string {
	if path == "" {
		msg := ("path to a file cannot be empty")
		slog.Error(msg)
		panic(msg)
	}

	defer func() {
		if err := recover(); err != nil {
			msg, _ := err.(string)
			slog.Error(msg, slog.String("path_to_convert", path))
			panic(msg)
		}
	}()

	absPath, err := filepath.Abs(path)
	if err != nil {
		panic("malformated file path; " + err.Error())
	}

	slashPath := filepath.ToSlash(absPath)

	if runtime.GOOS == "windows" && slashPath[0] != '/' {
		slashPath = "/" + slashPath
	}

	u := url.URL{
		Scheme: "file",
		Path:   slashPath,
	}

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

func mapToKeys[K comparable, V any](dict map[K]V) []K {
	list := make([]K, 0, len(dict))

	for key, _ := range dict {
		list = append(list, key)
	}

	return list
}

func createLogFile() *os.File {
	userCachePath, err := os.UserCacheDir()
	if err != nil {
		return os.Stdout
	}

	appCachePath := filepath.Join(userCachePath, "go-template-lsp")
	logFilePath := filepath.Join(appCachePath, "go-template-lsp.log")

	_ = os.Mkdir(appCachePath, os.ModePerm)

	fileInfo, err := os.Stat(logFilePath)
	if err == nil && fileInfo.Size() >= 5_000_000 { // if file exist and size > 5Mo, trunc/empty the file content
		file, err := os.OpenFile(logFilePath, os.O_TRUNC|os.O_WRONLY, os.ModePerm)
		if err != nil {
			return os.Stdout
		}
		return file
	}

	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, os.ModePerm)
	if err != nil {
		return os.Stdout
	}

	return file
}

func configureLogging() {
	file := createLogFile()
	if file == nil {
		file = os.Stdout
	}

	logger := slog.New(slog.NewJSONHandler(file, nil))
	slog.SetDefault(logger)
}

func GetServerGroupLogging[T any](storage *workSpaceStore, counter requestCounter, request lsp.RequestMessage[T], textFromClient map[string][]byte) slog.Attr {
	group := slog.Group("server",
		slog.String("root_path", storage.RootPath),
		slog.Any("last_request", request),
		slog.Any("open_files", mapToKeys(storage.RawFiles)),
		slog.Any("files_waiting_processing", mapToKeys(textFromClient)),
		slog.Any("request_counter", counter),
	)

	return group
}
