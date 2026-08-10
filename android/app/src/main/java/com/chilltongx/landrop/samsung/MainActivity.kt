package com.chilltongx.landrop.samsung

import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.FileProvider
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.chilltongx.landrop.samsung.model.CapturedPhoto
import com.chilltongx.landrop.samsung.ui.LanDropScreen
import com.chilltongx.landrop.samsung.ui.LanDropViewModel
import com.chilltongx.landrop.samsung.ui.theme.LanDropTheme
import java.io.File
import java.time.LocalDateTime
import java.time.format.DateTimeFormatter

class MainActivity : ComponentActivity() {
    private val viewModel: LanDropViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            LanDropTheme {
                val context = LocalContext.current
                val state by viewModel.uiState.collectAsStateWithLifecycle()
                var pendingPhoto by remember { mutableStateOf<CapturedPhoto?>(null) }
                var pendingLanAction by remember { mutableStateOf<(() -> Unit)?>(null) }

                val lanPermissionLauncher = rememberLauncherForActivityResult(
                    ActivityResultContracts.RequestPermission(),
                ) { granted ->
                    val action = pendingLanAction
                    pendingLanAction = null
                    if (granted) action?.invoke() else viewModel.onLanPermissionDenied()
                }
                val cameraLauncher = rememberLauncherForActivityResult(
                    ActivityResultContracts.TakePicture(),
                ) { saved ->
                    val photo = pendingPhoto
                    pendingPhoto = null
                    if (saved && photo != null) {
                        viewModel.setCapturedPhoto(photo.copy(size = photo.file.length()))
                    } else {
                        photo?.file?.delete()
                    }
                }
                val documentLauncher = rememberLauncherForActivityResult(
                    ActivityResultContracts.OpenDocument(),
                ) { uri: Uri? ->
                    if (uri != null) {
                        runCatching {
                            contentResolver.takePersistableUriPermission(
                                uri,
                                android.content.Intent.FLAG_GRANT_READ_URI_PERMISSION,
                            )
                        }
                        runWithLanPermission(
                            action = { viewModel.uploadDocument(uri) },
                            onPermissionNeeded = { action ->
                                pendingLanAction = action
                                lanPermissionLauncher.launch(LOCAL_NETWORK_PERMISSION)
                            },
                        )
                    }
                }

                fun launchCamera() {
                    viewModel.discardPhoto()
                    val directory = File(context.cacheDir, "camera").apply { mkdirs() }
                    val name = "LAN-Drop_${PHOTO_TIME.format(LocalDateTime.now())}.jpg"
                    val file = File(directory, name)
                    val uri = FileProvider.getUriForFile(context, "${context.packageName}.files", file)
                    pendingPhoto = CapturedPhoto(uri = uri, file = file, displayName = name, size = 0)
                    cameraLauncher.launch(uri)
                }

                LanDropScreen(
                    state = state,
                    messages = viewModel.messages,
                    onServerUrlChange = viewModel::updateServerUrl,
                    onTokenChange = viewModel::updateToken,
                    onConnect = {
                        runWithLanPermission(
                            action = viewModel::connect,
                            onPermissionNeeded = { action ->
                                pendingLanAction = action
                                lanPermissionLauncher.launch(LOCAL_NETWORK_PERMISSION)
                            },
                        )
                    },
                    onRefresh = {
                        runWithLanPermission(
                            action = { viewModel.refresh() },
                            onPermissionNeeded = { action ->
                                pendingLanAction = action
                                lanPermissionLauncher.launch(LOCAL_NETWORK_PERMISSION)
                            },
                        )
                    },
                    onTakePhoto = ::launchCamera,
                    onRetakePhoto = ::launchCamera,
                    onDiscardPhoto = viewModel::discardPhoto,
                    onUploadPhoto = {
                        runWithLanPermission(
                            action = viewModel::uploadPhoto,
                            onPermissionNeeded = { action ->
                                pendingLanAction = action
                                lanPermissionLauncher.launch(LOCAL_NETWORK_PERMISSION)
                            },
                        )
                    },
                    onPickFile = { documentLauncher.launch(arrayOf("*/*")) },
                    onCancelUpload = viewModel::cancelUpload,
                )
            }
        }
    }

    private fun runWithLanPermission(action: () -> Unit, onPermissionNeeded: ((() -> Unit) -> Unit)) {
        val needsPermission = Build.VERSION.SDK_INT >= 37 &&
            checkSelfPermission(LOCAL_NETWORK_PERMISSION) != PackageManager.PERMISSION_GRANTED
        if (needsPermission) onPermissionNeeded(action) else action()
    }

    private companion object {
        const val LOCAL_NETWORK_PERMISSION = "android.permission.ACCESS_LOCAL_NETWORK"
        val PHOTO_TIME: DateTimeFormatter = DateTimeFormatter.ofPattern("yyyyMMdd_HHmmss")
    }
}
