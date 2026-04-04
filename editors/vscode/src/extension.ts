import { ChildProcess, spawn } from 'child_process';
import { text } from 'stream/consumers';
import * as vscode from 'vscode';
import { Message, ErrorHandlerResult, ErrorHandler, LanguageClient, LanguageClientOptions, ServerOptions, CloseHandlerResult } from 'vscode-languageclient/node'

let clients = new Map<string, LanguageClient>()
let global_config: ConfigExtension

export async function activate(context: vscode.ExtensionContext) {
  global_config = getConfig()

  let outputChannel = global_config.outputChannel
  outputChannel.show()

  let error_status: boolean = false
  let err_message

  try {
    let command = spawn("go-template-lsp", ["-version"])
    command.on("error", (err) => {
      err_message = err
      error_status = true
    })

    global_config.lspVersion = await text(command.stdout.setEncoding("utf-8"))

  } catch (err) {
    err_message = err
    error_status = true
  }

  if (error_status) {
    console.error("Fatal, Go Template LSP executable not found. ", err_message)
    outputChannel.append("ERROR: Go Template LSP executable not found. " + err_message)
    vscode.window.showErrorMessage("Fatal, Go Template LSP executable not found. " + err_message)
    return
  }

  const disposable = vscode.commands.registerCommand('go-template-lsp.launch-debug', () => {
    vscode.window.showInformationMessage('Hello World from Go Template LSP!');
  });
  context.subscriptions.push(disposable);

  vscode.workspace.textDocuments.forEach(changedDocument)
  vscode.workspace.onDidOpenTextDocument(changedDocument)

  vscode.workspace.onDidChangeWorkspaceFolders((event) => {
    console.log(`workspace change ! \n event: ${event}`)
    console.log(event)

    for (const worksapceRemoved of event.removed) {
      let path = worksapceRemoved.uri.path

      let client = clients.get(path)
      client?.stop()
      clients.delete(path)
    }
  })
}

export function deactivate() {
  let promises: Thenable<void>[] = []

  for (const client of clients.values()) {
    promises.push(client.stop())
  }

  clients.clear()

  return Promise.all(promises).then(() => undefined)
}

function changedDocument(document: vscode.TextDocument): void {
  let outputChannel = global_config.outputChannel
  if (!outputChannel) {
    vscode.window.showErrorMessage("Go Template LSP 'output_channel' not found")
    console.log("Go Template LSP 'output_channel' not found")
    return
  }

  outputChannel.appendLine(`doc = ${document}`)
  outputChannel.appendLine(document.toString())

  if (document.languageId !== 'go-tmpl' && (document.uri.scheme !== 'file' && document.uri.scheme !== 'untitled'))
    return

  let status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 10)
  status.text = global_config.lspVersion
  status.show()

  let local_workspace = vscode.workspace.workspaceFolders?.find((workspace) => {
    return document.uri.path.startsWith(workspace.uri.path)
  })

  if (!local_workspace) return
  if (clients.get(local_workspace.uri.path)) return

  let config = global_config

  let executableName: string = getPlatformSpecificBinaryName(config.executableName)
  let serverOptions: ServerOptions = {
    run: { command: executableName },
    debug: { command: executableName },
  }

  let clientOptions: LanguageClientOptions = {
    documentSelector: [
      { language: config.languageId },
    ],
    workspaceFolder: local_workspace,
    outputChannel: outputChannel,
    diagnosticCollectionName: "go-template-lsp-" + local_workspace.uri.path,
    errorHandler: {
      error: function(error: Error, message: Message | undefined, count: number | undefined): ErrorHandlerResult {
        vscode.window.showErrorMessage("Go Template LSP error. " + message)

        let result: ErrorHandlerResult = { action: 1, message: `lsp error handling ... ${message}` }
        return result;
      },
      closed: (): CloseHandlerResult => {
        let result: CloseHandlerResult = { action: 1, message: `lsp shuting down ....` }
        let isWorkspaceFound: boolean = false

        for (const [key, val] of clients) {
          if (document.uri.path.startsWith(key)) {
            clients.delete(key)
            isWorkspaceFound = true

            break
          }
        }

        if (isWorkspaceFound == false) {
          result.message = result.message +
            "\n ---> unable to found workspace monitored by LSP \n current_workspace = " +
            document.uri.path
        }

        return result
      },
    }
  }

  let client = new LanguageClient(
    "Go Template LSP",
    serverOptions,
    clientOptions,
  )

  client.start()
  clients.set(local_workspace.uri.path, client)
}

function getPlatformSpecificBinaryName(baseBinaryName: string): string {
  const ext = (process.platform == "win32") ? ".exe" : ""
  return baseBinaryName + ext
}

//
// Config
//

interface ConfigExtension {
  readonly executableName: string;
  readonly outputChannel: vscode.OutputChannel;
  readonly languageId: string;
  lspVersion: string
}

function getConfig(): ConfigExtension {
  let config = vscode.workspace.getConfiguration("lsp")

  return {
    executableName: config.get<string>("executableName") || "go-template-lsp",
    outputChannel: vscode.window.createOutputChannel(config.get<string>("outputChannel") || "Go Template LSP"),
    languageId: "go-tmpl",
    lspVersion: "<unknown>",
  }
}
