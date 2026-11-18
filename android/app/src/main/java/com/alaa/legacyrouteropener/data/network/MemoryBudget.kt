package com.alaa.legacyrouteropener.data.network

import java.io.Closeable
import java.io.InputStream
import java.io.IOException
import java.io.OutputStream

internal const val MIB = 1024 * 1024

internal class BodyTooLarge : IOException("Body limit exceeded")
internal class BudgetExceeded : IOException("Buffer budget exhausted")

internal class MemoryBudget(private val limit: Int = 64 * MIB) {
    var used: Int = 0
        @Synchronized get
        private set

    @Synchronized
    fun reserve(size: Int): Closeable {
        if (size > limit - used) throw BudgetExceeded()
        used += size
        return object : Closeable {
            private var released = false

            override fun close() = synchronized(this@MemoryBudget) {
                if (!released) {
                    used -= size
                    released = true
                }
            }
        }
    }
}

internal class WireBody(private val budget: MemoryBudget, private val limit: Int = 32 * MIB) :
    OutputStream() {
    private class Slab(val bytes: ByteArray, val reservation: Closeable)
    private val slabs = mutableListOf<Slab>()
    private var closed = false
    var size = 0
        private set

    private fun capacity() {
        check(!closed)
        if (size == limit) throw BodyTooLarge()
        if (size % SLAB == 0) {
            val capacity = minOf(SLAB, limit - size)
            val reservation = budget.reserve(capacity)
            try {
                slabs.add(Slab(ByteArray(capacity), reservation))
            } catch (failure: Throwable) {
                reservation.close()
                throw failure
            }
        }
    }

    override fun write(value: Int) {
        capacity()
        slabs.last().bytes[size % SLAB] = value.toByte()
        size++
    }

    override fun write(bytes: ByteArray, off: Int, len: Int) {
        if (len > limit - size) throw BodyTooLarge()
        var copied = 0
        while (copied < len) {
            capacity()
            val offset = size % SLAB
            val slab = slabs.last().bytes
            val count = minOf(slab.size - offset, len - copied)
            bytes.copyInto(slab, offset, off + copied, off + copied + count)
            size += count
            copied += count
        }
    }

    fun writeTo(output: OutputStream) {
        slabs.forEachIndexed { index, slab ->
            output.write(slab.bytes, 0, minOf(slab.bytes.size, size - index * SLAB))
        }
    }

    fun takeFrom(): WireBody {
        check(!closed)
        val moved = WireBody(budget, limit)
        moved.slabs.addAll(slabs)
        moved.size = size
        slabs.clear()
        size = 0
        closed = true
        return moved
    }

    fun inputStream() = object : InputStream() {
        var position = 0
        override fun read(): Int {
            if (position == size) return -1
            val value = slabs[position / SLAB].bytes[position % SLAB]
            position++
            return value.toInt() and 255
        }

        override fun read(buffer: ByteArray, offset: Int, length: Int): Int {
            if (length == 0) return 0
            if (position == size) return -1
            val count = minOf(length, size - position, SLAB - position % SLAB)
            slabs[position / SLAB].bytes.copyInto(buffer, offset, position % SLAB, position % SLAB + count)
            position += count
            return count
        }
    }

    override fun close() {
        if (closed) return
        closed = true
        slabs.forEach { it.reservation.close() }
        slabs.clear()
        size = 0
    }

    private companion object {
        const val SLAB = 32 * 1024
    }
}
