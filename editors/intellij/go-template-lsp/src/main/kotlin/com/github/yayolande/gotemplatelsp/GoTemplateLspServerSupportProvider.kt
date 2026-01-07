package com.github.yayolande.gotemplatelsp

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.LspServerSupportProvider
import com.intellij.platform.lsp.api.ProjectWideLspServerDescriptor
// import com.intellij.ssh.ProcessBuilder
import com.intellij.notification.Notification
import com.intellij.notification.NotificationType
import com.intellij.openapi.diagnostic.Logger
import com.intellij.openapi.util.SystemInfo
import java.io.IOException
import java.util.Locale.getDefault

val supportedExtensions = arrayOf(
    "tpl",
    "tmpl",
    "gotmpl",
    "gohtml",
    "html",
)

val osName = System.getProperty("os.name").lowercase(getDefault())
val binayName = "go-template-lsp"

class GoTemplateLspServerSupportProvider : LspServerSupportProvider {
    override fun fileOpened(
        project: Project,
        file: VirtualFile,
        serverStarter: LspServerSupportProvider.LspServerStarter
    ) {
        val logger = Logger.getInstance(GoTemplateLspServerSupportProvider::class.java)
        logger.info("LSP server supporting file opened: $file")

        if (!isSupportedExtension(file.extension)) {
            return
        }

        var status = -1
        val executableName = if(SystemInfo.isWindows) "$binayName.exe" else binayName
        try {
            val process = ProcessBuilder(executableName, "-version").start()
            status = process.waitFor()
        } catch (e: IOException) {
            e.printStackTrace()
            status = -1
            logger.error(e.message)
        }

        if (status != 0) {
            Notification(
                "go-template-lsp.notifications",
                "Go Template LSP",
                "Executable not found. Install the LSP first",
                NotificationType.ERROR
            ).notify(project)

            logger.error("Go Template LSP executable not found")
            throw IllegalStateException("Go Template LSP binary not installed")
        }

        logger.info("starting $executableName server ...")
        serverStarter.ensureServerStarted(GoTemplateLspServerDescriptor(project, executableName))
    }
}

class GoTemplateLspServerDescriptor(project: Project, val executableName: String) : ProjectWideLspServerDescriptor(project, "go-template-lsp") {
    override fun isSupportedFile(file: VirtualFile): Boolean {
        return isSupportedExtension(file.extension)
    }

    override fun createCommandLine(): GeneralCommandLine = GeneralCommandLine(executableName)
}

fun isSupportedExtension(extension: String?): Boolean {
    return extension in supportedExtensions
    // return supportedExtensions.contains(extension)
}