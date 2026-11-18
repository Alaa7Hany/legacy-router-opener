package com.alaa.legacyrouteropener.data.network

import java.io.Closeable
import java.net.Socket
import java.net.SocketTimeoutException
import java.util.concurrent.ScheduledThreadPoolExecutor
import java.util.concurrent.TimeUnit

internal class SocketDeadline(private val socket: Socket, timeoutMs: Long) : Closeable {
    private val expires = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs)
    private val timer = executor.schedule({ runCatching { socket.close() } }, timeoutMs, TimeUnit.MILLISECONDS)

    fun check() {
        val remaining = expires - System.nanoTime()
        if (remaining <= 0) throw SocketTimeoutException()
        socket.soTimeout = TimeUnit.NANOSECONDS.toMillis(remaining).coerceAtLeast(1).toInt()
    }

    override fun close() {
        timer.cancel(false)
        if (!socket.isClosed) runCatching { socket.soTimeout = 0 }
    }

    private companion object {
        val executor = ScheduledThreadPoolExecutor(1) { task ->
            Thread(task, "router-deadlines").apply { isDaemon = true }
        }.apply { removeOnCancelPolicy = true }
    }
}

internal class SocketRegistry : Closeable {
    private val sockets = mutableSetOf<Socket>()
    @Volatile var closed = false
        private set

    @Synchronized
    fun <T : Socket> track(socket: T): T {
        if (closed) {
            socket.close()
            throw java.io.IOException("Session closed")
        }
        sockets.add(socket)
        return socket
    }

    @Synchronized
    fun forget(socket: Socket) {
        sockets.remove(socket)
    }

    override fun close() {
        val pending = synchronized(this) {
            closed = true
            sockets.toList().also { sockets.clear() }
        }
        pending.forEach { runCatching { it.close() } }
    }
}
