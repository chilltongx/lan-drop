package com.chilltongx.landrop.samsung.net

import android.content.ContentResolver
import android.database.Cursor
import android.net.Uri
import android.provider.OpenableColumns
import com.chilltongx.landrop.samsung.model.FileSnapshot
import com.chilltongx.landrop.samsung.model.RemoteFile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.io.BufferedReader
import java.io.DataOutputStream
import java.io.InputStreamReader
import java.net.HttpURLConnection
import java.net.URL
import java.nio.charset.StandardCharsets
import java.time.Instant
import java.util.UUID

class LanDropClient(private val contentResolver: ContentResolver) {
    suspend fun listFiles(endpoint: ServerEndpoint, token: String): FileSnapshot = withContext(Dispatchers.IO) {
        val connection = open(endpoint.api("/api/files"), token, "GET")
        try {
            val body = connection.readResponse()
            val root = JSONObject(body)
            val files = root.getJSONArray("files")
            FileSnapshot(
                files = buildList {
                    for (index in 0 until files.length()) {
                        val file = files.getJSONObject(index)
                        add(
                            RemoteFile(
                                name = file.getString("name"),
                                size = file.getLong("size"),
                                modified = Instant.parse(file.getString("modified")),
                                sha256 = file.getString("sha256"),
                            ),
                        )
                    }
                },
                count = root.getInt("count"),
                totalSize = root.getLong("totalSize"),
                maxBytes = root.getLong("maxBytes"),
            )
        } finally {
            connection.disconnect()
        }
    }

    suspend fun upload(
        endpoint: ServerEndpoint,
        token: String,
        uri: Uri,
        displayName: String,
        mimeType: String,
        size: Long,
        onProgress: (Float) -> Unit,
    ): RemoteFile = withContext(Dispatchers.IO) {
        val boundary = "LanDrop-${UUID.randomUUID()}"
        val connection = open(endpoint.api("/api/files"), token, "POST").apply {
            doOutput = true
            readTimeout = 120_000
            setChunkedStreamingMode(64 * 1024)
            setRequestProperty("Content-Type", "multipart/form-data; boundary=$boundary")
        }
        try {
            DataOutputStream(connection.outputStream).use { output ->
                val safeName = displayName.substringAfterLast('/').substringAfterLast('\\')
                    .replace(Regex("[\\r\\n\"]"), "_")
                output.writeUtf8("--$boundary\r\n")
                output.writeUtf8("Content-Disposition: form-data; name=\"file\"; filename=\"$safeName\"\r\n")
                output.writeUtf8("Content-Type: $mimeType\r\n\r\n")
                val input = contentResolver.openInputStream(uri)
                    ?: throw IllegalStateException("无法读取所选文件")
                input.use {
                    val buffer = ByteArray(64 * 1024)
                    var uploaded = 0L
                    while (true) {
                        currentCoroutineContext().ensureActive()
                        val read = it.read(buffer)
                        if (read < 0) break
                        output.write(buffer, 0, read)
                        uploaded += read
                        if (size > 0) onProgress((uploaded.toDouble() / size).toFloat().coerceIn(0f, 1f))
                    }
                }
                output.writeUtf8("\r\n--$boundary--\r\n")
                output.flush()
            }
            val result = JSONObject(connection.readResponse())
            RemoteFile(
                name = result.getString("name"),
                size = result.getLong("size"),
                modified = Instant.parse(result.getString("modified")),
                sha256 = result.getString("sha256"),
            )
        } finally {
            connection.disconnect()
        }
    }

    suspend fun observeFiles(endpoint: ServerEndpoint, token: String, onChanged: () -> Unit) {
        while (currentCoroutineContext().isActive) {
            val connection = open(endpoint.api("/api/events"), token, "GET").apply {
                readTimeout = 25_000
                setRequestProperty("Accept", "text/event-stream")
            }
            try {
                val status = connection.responseCode
                if (status !in 200..299) throw ApiException(status, connection.errorMessage())
                BufferedReader(InputStreamReader(connection.inputStream, StandardCharsets.UTF_8)).use { reader ->
                    var data: String? = null
                    while (currentCoroutineContext().isActive) {
                        val line = reader.readLine() ?: break
                        when {
                            line.startsWith("data: ") -> data = line.removePrefix("data: ")
                            line.isEmpty() && data != null -> {
                                val kind = JSONObject(data).optString("kind")
                                if (kind == "created" || kind == "deleted") onChanged()
                                data = null
                            }
                        }
                    }
                }
            } finally {
                connection.disconnect()
            }
            if (currentCoroutineContext().isActive) delay(1_000)
        }
    }

    fun metadata(uri: Uri): LocalFileMetadata {
        var name: String? = null
        var size = -1L
        val projection = arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE)
        val cursor: Cursor? = contentResolver.query(uri, projection, null, null, null)
        cursor?.use {
            if (it.moveToFirst()) {
                name = it.getString(it.getColumnIndexOrThrow(OpenableColumns.DISPLAY_NAME))
                size = it.getLong(it.getColumnIndexOrThrow(OpenableColumns.SIZE))
            }
        }
        return LocalFileMetadata(
            displayName = name?.takeIf(String::isNotBlank) ?: "LAN-Drop-file",
            size = size,
            mimeType = contentResolver.getType(uri) ?: "application/octet-stream",
        )
    }

    private fun open(url: String, token: String, method: String): HttpURLConnection =
        (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = method
            connectTimeout = 8_000
            readTimeout = 20_000
            useCaches = false
            instanceFollowRedirects = false
            setRequestProperty("Accept", "application/json")
            setRequestProperty("X-Share-Token", token)
        }

    private fun HttpURLConnection.readResponse(): String {
        val status = responseCode
        val stream = if (status in 200..299) inputStream else errorStream
        val body = stream?.bufferedReader(StandardCharsets.UTF_8)?.use { it.readText() }.orEmpty()
        if (status !in 200..299) throw ApiException(status, errorMessage(body))
        return body
    }

    private fun HttpURLConnection.errorMessage(body: String = ""): String = runCatching {
        JSONObject(body).optString("error").takeIf(String::isNotBlank)
    }.getOrNull() ?: when (responseCode) {
        401 -> "连接码不正确或已失效"
        413 -> "文件超过服务器大小限制"
        429 -> "连接尝试过多，请稍后再试"
        else -> "服务器返回 HTTP $responseCode"
    }
}

data class LocalFileMetadata(
    val displayName: String,
    val size: Long,
    val mimeType: String,
)

class ApiException(val status: Int, message: String) : Exception(message)

private fun DataOutputStream.writeUtf8(value: String) {
    write(value.toByteArray(StandardCharsets.UTF_8))
}
