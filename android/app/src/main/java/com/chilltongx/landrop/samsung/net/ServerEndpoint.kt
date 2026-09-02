package com.chilltongx.landrop.samsung.net

import java.net.URI
import java.net.URLDecoder
import java.nio.charset.StandardCharsets

data class ServerEndpoint(
    val baseUrl: String,
    val tokenFromUrl: String?,
    val usesCleartext: Boolean,
) {
    fun api(path: String): String = "$baseUrl${if (path.startsWith('/')) path else "/$path"}"
}

object ServerEndpointPolicy {
    private val tokenPattern = Regex("^[A-Za-z0-9_-]{4,64}$")
    private val ipv4Pattern = Regex("^(\\d{1,3})\\.(\\d{1,3})\\.(\\d{1,3})\\.(\\d{1,3})$")

    fun parse(rawValue: String): ServerEndpoint {
        val trimmed = rawValue.trim()
        require(trimmed.isNotEmpty()) { "请输入服务器地址" }
        val withScheme = if (trimmed.contains("://")) trimmed else "http://$trimmed"
        val uri = runCatching { URI(withScheme) }
            .getOrElse { throw IllegalArgumentException("服务器地址格式不正确") }
        val scheme = uri.scheme?.lowercase()
        require(scheme == "http" || scheme == "https") { "服务器地址只支持 HTTP 或 HTTPS" }
        val host = uri.host?.lowercase()
        require(!host.isNullOrBlank()) { "服务器地址缺少主机名" }
        require(uri.userInfo == null) { "服务器地址不能包含用户名或密码" }
        require(uri.fragment == null) { "服务器地址不能包含片段" }
        require(uri.path.isNullOrEmpty() || uri.path == "/") { "bigbang 服务器必须部署在网站根路径" }
        if (scheme == "http") {
            require(isLocalHost(host)) { "公网地址必须使用 HTTPS；HTTP 仅允许局域网地址" }
        }

        val port = uri.port
        require(port == -1 || port in 1..65535) { "服务器端口不正确" }
        val normalized = URI(scheme, null, host, port, null, null, null).toString().trimEnd('/')
        return ServerEndpoint(
            baseUrl = normalized,
            tokenFromUrl = queryParameter(uri.rawQuery, "token")?.takeIf(::isValidToken),
            usesCleartext = scheme == "http",
        )
    }

    fun isValidToken(value: String): Boolean = tokenPattern.matches(value.trim())

    private fun isLocalHost(host: String): Boolean {
        if (host == "localhost" || host.endsWith(".local") || !host.contains('.')) return true
        val ipv4 = ipv4Pattern.matchEntire(host)?.groupValues?.drop(1)?.map(String::toIntOrNull)
        if (ipv4 != null && ipv4.all { it != null && it in 0..255 }) {
            val octets = ipv4.filterNotNull()
            return octets[0] == 10 ||
                octets[0] == 127 ||
                octets[0] == 192 && octets[1] == 168 ||
                octets[0] == 172 && octets[1] in 16..31 ||
                octets[0] == 169 && octets[1] == 254 ||
                octets[0] == 100 && octets[1] in 64..127
        }
        val ipv6 = host.lowercase()
        return ipv6 == "::1" || ipv6.startsWith("fe8") || ipv6.startsWith("fe9") ||
            ipv6.startsWith("fea") || ipv6.startsWith("feb") ||
            ipv6.startsWith("fc") || ipv6.startsWith("fd")
    }

    private fun queryParameter(rawQuery: String?, name: String): String? = rawQuery
        ?.split('&')
        ?.mapNotNull { part ->
            val pieces = part.split('=', limit = 2)
            if (decode(pieces[0]) != name) null else decode(pieces.getOrElse(1) { "" })
        }
        ?.firstOrNull()

    private fun decode(value: String): String =
        URLDecoder.decode(value, StandardCharsets.UTF_8.name())
}
