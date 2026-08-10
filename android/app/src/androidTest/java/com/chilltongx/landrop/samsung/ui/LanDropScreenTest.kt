package com.chilltongx.landrop.samsung.ui

import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.chilltongx.landrop.samsung.ui.theme.LanDropTheme
import kotlinx.coroutines.flow.MutableSharedFlow
import org.junit.Rule
import org.junit.Test

class LanDropScreenTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun photoButtonFollowsConnectionState() {
        val messages = MutableSharedFlow<String>()
        var status by mutableStateOf(ConnectionStatus.DISCONNECTED)
        compose.setContent {
            LanDropTheme {
                LanDropScreen(
                    state = LanDropUiState(connection = status),
                    messages = messages,
                    onServerUrlChange = {},
                    onTokenChange = {},
                    onConnect = {},
                    onRefresh = {},
                    onTakePhoto = {},
                    onRetakePhoto = {},
                    onDiscardPhoto = {},
                    onUploadPhoto = {},
                    onPickFile = {},
                    onCancelUpload = {},
                )
            }
        }
        compose.onNodeWithTag("take_photo").assertIsNotEnabled()

        compose.runOnIdle { status = ConnectionStatus.CONNECTED }
        compose.onNodeWithTag("take_photo").assertIsEnabled()
    }
}
