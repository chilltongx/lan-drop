package com.chilltongx.landrop.samsung.ui

import android.app.Application
import android.net.Uri
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.chilltongx.landrop.samsung.data.SettingsStore
import com.chilltongx.landrop.samsung.model.CapturedPhoto
import com.chilltongx.landrop.samsung.model.RemoteFile
import com.chilltongx.landrop.samsung.net.LanDropClient
import com.chilltongx.landrop.samsung.net.ServerEndpoint
import com.chilltongx.landrop.samsung.net.ServerEndpointPolicy
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

class LanDropViewModel(application: Application) : AndroidViewModel(application) {
    private val client = LanDropClient(application.contentResolver)
    private val settings = SettingsStore(application)
    private val saved = settings.load()
    private val _uiState = MutableStateFlow(
        LanDropUiState(serverUrl = saved.serverUrl),
    )
    val uiState: StateFlow<LanDropUiState> = _uiState.asStateFlow()

    private val _messages = MutableSharedFlow<String>(extraBufferCapacity = 8)
    val messages: SharedFlow<String> = _messages.asSharedFlow()

    private var endpoint: ServerEndpoint? = null
    private var eventJob: Job? = null
    private var refreshJob: Job? = null
    private var uploadJob: Job? = null

    fun updateServerUrl(value: String) {
        _uiState.update { it.copy(serverUrl = value) }
    }

    fun updateToken(value: String) {
        _uiState.update { it.copy(token = value) }
    }

    fun connect() {
        if (_uiState.value.connection == ConnectionStatus.CONNECTING) return
        val parsed = runCatching { ServerEndpointPolicy.parse(_uiState.value.serverUrl) }
            .getOrElse {
                _messages.tryEmit(it.message ?: "服务器地址不正确")
                return
            }
        val token = _uiState.value.token.trim().ifEmpty { parsed.tokenFromUrl.orEmpty() }
        if (!ServerEndpointPolicy.isValidToken(token)) {
            _messages.tryEmit("连接码应为 4–64 位字母、数字、连字符或下划线")
            return
        }

        eventJob?.cancel()
        refreshJob?.cancel()
        endpoint = parsed
        _uiState.update {
            it.copy(
                serverUrl = parsed.baseUrl,
                token = token,
                connection = ConnectionStatus.CONNECTING,
                cleartextWarning = parsed.usesCleartext,
            )
        }
        refreshJob = viewModelScope.launch {
            try {
                val snapshot = client.listFiles(parsed, token)
                settings.save(parsed.baseUrl)
                _uiState.update {
                    it.copy(
                        connection = ConnectionStatus.CONNECTED,
                        files = snapshot.files,
                        totalSize = snapshot.totalSize,
                        maxBytes = snapshot.maxBytes,
                    )
                }
                startEvents(parsed, token)
                _messages.emit("已连接到 bigbang")
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                endpoint = null
                _uiState.update { it.copy(connection = ConnectionStatus.ERROR) }
                _messages.emit(error.message ?: "连接失败")
            }
        }
    }

    fun refresh(showErrors: Boolean = true) {
        val currentEndpoint = endpoint ?: return
        if (_uiState.value.connection == ConnectionStatus.CONNECTING) return
        refreshJob?.cancel()
        refreshJob = viewModelScope.launch {
            try {
                val snapshot = client.listFiles(currentEndpoint, _uiState.value.token)
                _uiState.update {
                    it.copy(
                        connection = ConnectionStatus.CONNECTED,
                        files = snapshot.files,
                        totalSize = snapshot.totalSize,
                        maxBytes = snapshot.maxBytes,
                    )
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                if (showErrors) _messages.emit(error.message ?: "刷新失败")
            }
        }
    }

    fun uploadDocument(uri: Uri) {
        val metadata = runCatching { client.metadata(uri) }.getOrElse {
            _messages.tryEmit(it.message ?: "无法读取文件信息")
            return
        }
        upload(uri, metadata.displayName, metadata.mimeType, metadata.size, deleteAfter = null)
    }

    fun setCapturedPhoto(photo: CapturedPhoto) {
        _uiState.update { it.copy(capturedPhoto = photo) }
    }

    fun discardPhoto() {
        _uiState.value.capturedPhoto?.file?.delete()
        _uiState.update { it.copy(capturedPhoto = null) }
    }

    fun uploadPhoto() {
        val photo = _uiState.value.capturedPhoto ?: return
        upload(photo.uri, photo.displayName, "image/jpeg", photo.size, deleteAfter = photo)
    }

    fun cancelUpload() {
        uploadJob?.cancel()
        uploadJob = null
        _uiState.update { it.copy(isUploading = false, uploadProgress = 0f, uploadName = "") }
        _messages.tryEmit("已取消上传")
    }

    fun onLanPermissionDenied() {
        _messages.tryEmit("需要“附近设备”权限才能连接局域网服务器")
    }

    private fun upload(
        uri: Uri,
        displayName: String,
        mimeType: String,
        size: Long,
        deleteAfter: CapturedPhoto?,
    ) {
        val currentEndpoint = endpoint
        if (currentEndpoint == null || _uiState.value.connection != ConnectionStatus.CONNECTED) {
            _messages.tryEmit("请先连接 bigbang 服务器")
            return
        }
        if (_uiState.value.isUploading) {
            _messages.tryEmit("已有文件正在上传")
            return
        }
        if (size > 0 && size > _uiState.value.maxBytes) {
            _messages.tryEmit("文件超过服务器大小限制")
            return
        }

        uploadJob = viewModelScope.launch {
            _uiState.update {
                it.copy(isUploading = true, uploadProgress = 0f, uploadName = displayName)
            }
            try {
                val uploaded = client.upload(
                    endpoint = currentEndpoint,
                    token = _uiState.value.token,
                    uri = uri,
                    displayName = displayName,
                    mimeType = mimeType,
                    size = size,
                    onProgress = { progress -> _uiState.update { it.copy(uploadProgress = progress) } },
                )
                deleteAfter?.file?.delete()
                _uiState.update {
                    it.copy(
                        capturedPhoto = if (deleteAfter == null) it.capturedPhoto else null,
                        isUploading = false,
                        uploadProgress = 1f,
                        uploadName = "",
                    )
                }
                refresh(showErrors = false)
                _messages.emit("${uploaded.name} 上传完成")
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                _uiState.update { it.copy(isUploading = false, uploadProgress = 0f, uploadName = "") }
                _messages.emit(error.message ?: "上传失败")
            }
        }
    }

    private fun startEvents(currentEndpoint: ServerEndpoint, token: String) {
        eventJob?.cancel()
        eventJob = viewModelScope.launch {
            while (isActive) {
                try {
                    client.observeFiles(currentEndpoint, token) {
                        refresh(showErrors = false)
                    }
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (_: Exception) {
                    // SSE 断线后退避重连；文件操作仍可通过手动刷新完成。
                }
                if (isActive) delay(1_500)
            }
        }
    }

    override fun onCleared() {
        _uiState.value.capturedPhoto?.file?.delete()
    }
}

data class LanDropUiState(
    val serverUrl: String = "",
    val token: String = "",
    val connection: ConnectionStatus = ConnectionStatus.DISCONNECTED,
    val cleartextWarning: Boolean = false,
    val files: List<RemoteFile> = emptyList(),
    val totalSize: Long = 0,
    val maxBytes: Long = Long.MAX_VALUE,
    val capturedPhoto: CapturedPhoto? = null,
    val isUploading: Boolean = false,
    val uploadProgress: Float = 0f,
    val uploadName: String = "",
)

enum class ConnectionStatus {
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
    ERROR,
}
