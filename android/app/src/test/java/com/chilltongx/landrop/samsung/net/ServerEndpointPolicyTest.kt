package com.chilltongx.landrop.samsung.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class ServerEndpointPolicyTest {
    @Test
    fun normalizesPrivateLanAddressWithoutScheme() {
        val endpoint = ServerEndpointPolicy.parse("192.168.1.20:8080/")

        assertEquals("http://192.168.1.20:8080", endpoint.baseUrl)
        assertTrue(endpoint.usesCleartext)
        assertNull(endpoint.tokenFromUrl)
    }

    @Test
    fun extractsTokenFromQrShareUrl() {
        val endpoint = ServerEndpointPolicy.parse("http://10.0.0.8:8080/?token=482901")

        assertEquals("http://10.0.0.8:8080", endpoint.baseUrl)
        assertEquals("482901", endpoint.tokenFromUrl)
    }

    @Test
    fun permitsHttpsForPublicHost() {
        val endpoint = ServerEndpointPolicy.parse("https://drop.example.com")

        assertFalse(endpoint.usesCleartext)
    }

    @Test
    fun rejectsCleartextPublicHost() {
        assertThrows(IllegalArgumentException::class.java) {
            ServerEndpointPolicy.parse("http://example.com")
        }
    }

    @Test
    fun rejectsNonRootDeploymentAndInvalidToken() {
        assertThrows(IllegalArgumentException::class.java) {
            ServerEndpointPolicy.parse("http://192.168.1.20:8080/drop")
        }
        assertFalse(ServerEndpointPolicy.isValidToken("12"))
        assertTrue(ServerEndpointPolicy.isValidToken("room_2026"))
    }
}
