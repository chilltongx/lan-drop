package com.chilltongx.landrop.samsung.model

import android.net.Uri
import java.io.File
import java.time.Instant

data class RemoteFile(
    val name: String,
    val size: Long,
    val modified: Instant,
    val sha256: String,
)

data class FileSnapshot(
    val files: List<RemoteFile>,
    val count: Int,
    val totalSize: Long,
    val maxBytes: Long,
)

data class CapturedPhoto(
    val uri: Uri,
    val file: File,
    val displayName: String,
    val size: Long,
)
