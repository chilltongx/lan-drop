package com.chilltongx.landrop.samsung.ui

import android.content.ContentResolver
import android.util.Size
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.outlined.CameraAlt
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.PhotoCamera
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.chilltongx.landrop.samsung.model.CapturedPhoto
import com.chilltongx.landrop.samsung.model.RemoteFile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.withContext
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LanDropScreen(
    state: LanDropUiState,
    messages: SharedFlow<String>,
    onServerUrlChange: (String) -> Unit,
    onTokenChange: (String) -> Unit,
    onConnect: () -> Unit,
    onRefresh: () -> Unit,
    onTakePhoto: () -> Unit,
    onRetakePhoto: () -> Unit,
    onDiscardPhoto: () -> Unit,
    onUploadPhoto: () -> Unit,
    onPickFile: () -> Unit,
    onCancelUpload: () -> Unit,
) {
    val snackbar = remember { SnackbarHostState() }
    LaunchedEffect(messages) {
        messages.collect { snackbar.showSnackbar(it) }
    }

    Scaffold(
        contentWindowInsets = WindowInsets.safeDrawing,
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("LAN Drop", fontWeight = FontWeight.SemiBold)
                        Text(
                            "Samsung 手机端",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                },
                actions = { ConnectionBadge(state.connection) },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = MaterialTheme.colorScheme.background,
                ),
            )
        },
    ) { innerPadding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
            contentPadding = PaddingValues(start = 20.dp, end = 20.dp, top = 12.dp, bottom = 40.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            item {
                ConnectionCard(
                    state = state,
                    onServerUrlChange = onServerUrlChange,
                    onTokenChange = onTokenChange,
                    onConnect = onConnect,
                )
            }
            item {
                QuickTransferCard(
                    enabled = state.connection == ConnectionStatus.CONNECTED && !state.isUploading,
                    onTakePhoto = onTakePhoto,
                    onPickFile = onPickFile,
                )
            }
            state.capturedPhoto?.let { photo ->
                item {
                    PhotoReviewCard(
                        photo = photo,
                        uploading = state.isUploading,
                        onRetake = onRetakePhoto,
                        onDiscard = onDiscardPhoto,
                        onUpload = onUploadPhoto,
                    )
                }
            }
            if (state.isUploading) {
                item {
                    UploadProgressCard(state, onCancelUpload)
                }
            }
            item {
                FileSectionHeader(state, onRefresh)
            }
            if (state.files.isEmpty()) {
                item { EmptyFiles(state.connection == ConnectionStatus.CONNECTED) }
            } else {
                items(state.files, key = RemoteFile::name) { file ->
                    FileRow(file)
                }
            }
        }
    }
}

@Composable
private fun ConnectionBadge(status: ConnectionStatus) {
    val connected = status == ConnectionStatus.CONNECTED
    val color = when (status) {
        ConnectionStatus.CONNECTED -> MaterialTheme.colorScheme.primary
        ConnectionStatus.ERROR -> MaterialTheme.colorScheme.error
        else -> MaterialTheme.colorScheme.onSurfaceVariant
    }
    val label = when (status) {
        ConnectionStatus.DISCONNECTED -> "未连接"
        ConnectionStatus.CONNECTING -> "连接中"
        ConnectionStatus.CONNECTED -> "已连接"
        ConnectionStatus.ERROR -> "连接失败"
    }
    Surface(
        modifier = Modifier.padding(end = 16.dp),
        shape = RoundedCornerShape(999.dp),
        color = color.copy(alpha = 0.1f),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 7.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (status == ConnectionStatus.CONNECTING) {
                CircularProgressIndicator(modifier = Modifier.size(10.dp), strokeWidth = 1.5.dp)
            } else {
                Box(
                    Modifier
                        .size(8.dp)
                        .background(if (connected) color else color.copy(alpha = 0.65f), CircleShape),
                )
            }
            Spacer(Modifier.width(7.dp))
            Text(label, style = MaterialTheme.typography.labelMedium, color = color)
        }
    }
}

@Composable
private fun ConnectionCard(
    state: LanDropUiState,
    onServerUrlChange: (String) -> Unit,
    onTokenChange: (String) -> Unit,
    onConnect: () -> Unit,
) {
    Card(
        shape = RoundedCornerShape(24.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainer),
    ) {
        Column(
            modifier = Modifier.padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.Link, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                Spacer(Modifier.width(10.dp))
                Column {
                    Text("连接电脑", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                    Text(
                        "输入电脑上 LAN Drop 显示的地址和连接码",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            OutlinedTextField(
                value = state.serverUrl,
                onValueChange = onServerUrlChange,
                modifier = Modifier.fillMaxWidth(),
                label = { Text("服务器地址") },
                placeholder = { Text("192.168.1.20:8080") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            )
            OutlinedTextField(
                value = state.token,
                onValueChange = onTokenChange,
                modifier = Modifier.fillMaxWidth(),
                label = { Text("连接码") },
                leadingIcon = { Icon(Icons.Outlined.Lock, contentDescription = null) },
                singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            )
            Button(
                onClick = onConnect,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(52.dp),
                enabled = state.connection != ConnectionStatus.CONNECTING,
                shape = RoundedCornerShape(16.dp),
            ) {
                Text(if (state.connection == ConnectionStatus.CONNECTED) "重新连接" else "连接服务器")
            }
            if (state.cleartextWarning) {
                Text(
                    "当前为 HTTP，仅应在可信局域网中使用。照片和连接码不会被加密。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                )
            }
        }
    }
}

@Composable
private fun QuickTransferCard(enabled: Boolean, onTakePhoto: () -> Unit, onPickFile: () -> Unit) {
    Card(
        shape = RoundedCornerShape(24.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer),
    ) {
        Column(modifier = Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            Column {
                Text("手机直传", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
                Text(
                    if (enabled) "拍照后确认，即刻传到电脑" else "连接服务器后即可拍照上传",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.72f),
                )
            }
            Button(
                onClick = onTakePhoto,
                enabled = enabled,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(58.dp)
                    .testTag("take_photo"),
                shape = RoundedCornerShape(18.dp),
            ) {
                Icon(Icons.Outlined.PhotoCamera, contentDescription = null)
                Spacer(Modifier.width(10.dp))
                Text("拍照上传", style = MaterialTheme.typography.titleMedium)
            }
            FilledTonalButton(
                onClick = onPickFile,
                enabled = enabled,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(52.dp),
                shape = RoundedCornerShape(16.dp),
            ) {
                Icon(Icons.Outlined.CloudUpload, contentDescription = null)
                Spacer(Modifier.width(10.dp))
                Text("选择文件")
            }
        }
    }
}

@Composable
private fun PhotoReviewCard(
    photo: CapturedPhoto,
    uploading: Boolean,
    onRetake: () -> Unit,
    onDiscard: () -> Unit,
    onUpload: () -> Unit,
) {
    val resolver = LocalContext.current.contentResolver
    val preview by rememberPhotoPreview(resolver, photo)
    Card(shape = RoundedCornerShape(24.dp)) {
        Column {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(240.dp)
                    .background(MaterialTheme.colorScheme.surfaceVariant),
                contentAlignment = Alignment.Center,
            ) {
                if (preview != null) {
                    Image(
                        bitmap = preview!!,
                        contentDescription = "待上传照片预览",
                        modifier = Modifier.fillMaxSize(),
                        contentScale = ContentScale.Crop,
                    )
                } else {
                    Icon(
                        Icons.Outlined.CameraAlt,
                        contentDescription = null,
                        modifier = Modifier.size(48.dp),
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            Column(modifier = Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                Column {
                    Text("确认这张照片", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.SemiBold)
                    Text(
                        "${photo.displayName} · ${formatSize(photo.size)}",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                Button(
                    onClick = onUpload,
                    enabled = !uploading,
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(54.dp),
                    shape = RoundedCornerShape(16.dp),
                ) {
                    Icon(Icons.Outlined.CloudUpload, contentDescription = null)
                    Spacer(Modifier.width(8.dp))
                    Text("上传照片")
                }
                Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(onClick = onRetake, enabled = !uploading, modifier = Modifier.weight(1f)) {
                        Text("重拍")
                    }
                    TextButton(onClick = onDiscard, enabled = !uploading, modifier = Modifier.weight(1f)) {
                        Text("取消")
                    }
                }
            }
        }
    }
}

@Composable
private fun rememberPhotoPreview(resolver: ContentResolver, photo: CapturedPhoto) = produceState<ImageBitmap?>(
    initialValue = null,
    key1 = photo.uri,
) {
    value = withContext(Dispatchers.IO) {
        runCatching {
            resolver.loadThumbnail(photo.uri, Size(1440, 1440), null).asImageBitmap()
        }.getOrNull()
    }
}

@Composable
private fun UploadProgressCard(state: LanDropUiState, onCancel: () -> Unit) {
    Card(shape = RoundedCornerShape(20.dp)) {
        Column(modifier = Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("正在上传", fontWeight = FontWeight.SemiBold)
                    Text(
                        state.uploadName,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                Text("${(state.uploadProgress * 100).toInt()}%", style = MaterialTheme.typography.labelLarge)
            }
            LinearProgressIndicator(
                progress = { state.uploadProgress },
                modifier = Modifier.fillMaxWidth(),
            )
            TextButton(onClick = onCancel, contentPadding = PaddingValues(0.dp)) { Text("取消上传") }
        }
    }
}

@Composable
private fun FileSectionHeader(state: LanDropUiState, onRefresh: () -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.weight(1f)) {
            Text("电脑中的文件", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.SemiBold)
            Text(
                "${state.files.size} 个文件 · ${formatSize(state.totalSize)}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        IconButton(onClick = onRefresh, enabled = state.connection == ConnectionStatus.CONNECTED) {
            Icon(Icons.Outlined.Refresh, contentDescription = "刷新文件列表")
        }
    }
}

@Composable
private fun EmptyFiles(connected: Boolean) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(20.dp),
        color = MaterialTheme.colorScheme.surfaceContainer,
    ) {
        Column(
            modifier = Modifier.padding(28.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Icon(
                Icons.AutoMirrored.Outlined.InsertDriveFile,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(10.dp))
            Text(if (connected) "还没有文件" else "连接后显示电脑文件")
        }
    }
}

@Composable
private fun FileRow(file: RemoteFile) {
    Surface(
        shape = RoundedCornerShape(18.dp),
        color = MaterialTheme.colorScheme.surfaceContainer,
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier
                    .size(42.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(MaterialTheme.colorScheme.primaryContainer),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    Icons.AutoMirrored.Outlined.InsertDriveFile,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(file.name, maxLines = 1, overflow = TextOverflow.Ellipsis, fontWeight = FontWeight.Medium)
                Text(
                    "${formatSize(file.size)} · ${FILE_DATE.format(file.modified.atZone(ZoneId.systemDefault()))}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

private fun formatSize(bytes: Long): String {
    if (bytes < 0) return "大小未知"
    if (bytes < 1024) return "$bytes B"
    val units = arrayOf("KB", "MB", "GB", "TB")
    var value = bytes.toDouble()
    var index = -1
    while (value >= 1024 && index < units.lastIndex) {
        value /= 1024
        index++
    }
    return String.format(Locale.getDefault(), if (value >= 10) "%.0f %s" else "%.1f %s", value, units[index])
}

private val FILE_DATE: DateTimeFormatter = DateTimeFormatter.ofPattern("MM-dd HH:mm")
