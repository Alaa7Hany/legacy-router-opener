import http from 'node:http';
import https from 'node:https';
import crypto from 'node:crypto';
import zlib from 'node:zlib';
import { URL } from 'node:url';

const rawInput = (process.argv[2] || process.env.ROUTER_IP || '192.168.1.1').trim();
const hasExplicitHttp = /^http:\/\//i.test(rawInput);
const hasExplicitHttps = /^https:\/\//i.test(rawInput);
const rawRouterArg = rawInput
  .replace(/^https?:\/\//i, '')
  .replace(/\/.*$/, '')
  .trim();

let parsedHost = rawRouterArg;
let parsedPort = NaN;
if (rawRouterArg.startsWith('[')) {
  const closeBracketIdx = rawRouterArg.indexOf(']');
  if (closeBracketIdx > 0) {
    const afterBracket = rawRouterArg.slice(closeBracketIdx + 1);
    if (afterBracket.startsWith(':') && /^\d+$/.test(afterBracket.slice(1))) {
      parsedHost = rawRouterArg.slice(0, closeBracketIdx + 1);
      parsedPort = parseInt(afterBracket.slice(1), 10);
    }
  }
} else {
  const colonIdx = rawRouterArg.lastIndexOf(':');
  if (colonIdx > 0 && /^\d+$/.test(rawRouterArg.slice(colonIdx + 1))) {
    parsedHost = rawRouterArg.slice(0, colonIdx).trim();
    parsedPort = parseInt(rawRouterArg.slice(colonIdx + 1), 10);
  }
}

const hasValidCustomPort = Number.isInteger(parsedPort) && parsedPort >= 1 && parsedPort <= 65535;
const ROUTER_IP = (hasValidCustomPort ? parsedHost : rawRouterArg) || '192.168.1.1';
const HAS_EXPLICIT_PORT_OR_SCHEME = hasExplicitHttp || hasExplicitHttps || hasValidCustomPort;

let usePlainHttp = hasExplicitHttp || (hasValidCustomPort && parsedPort === 80 && !hasExplicitHttps);
let activeRouterPort = hasValidCustomPort ? parsedPort : (usePlainHttp ? 80 : 443);
const DEFAULT_PROXY_PORT = parseInt(process.env.PROXY_PORT || '8080', 10);
let activeProxyPort = DEFAULT_PROXY_PORT;

const HUAWEI_INITIAL_COOKIES = [
  ['FirstMenu', 'Admin_0'],
  ['SecondMenu', 'Admin_0_0'],
  ['ThirdMenu', 'Admin_0_0_0'],
  ['Language', 'en'],
];

function escapeRegExp(str) {
  return String(str).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function isConnectionRefusedError(err) {
  if (!err) return false;
  if (err.code === 'ECONNREFUSED') return true;
  if (Array.isArray(err.errors)) {
    return err.errors.some((e) => e && e.code === 'ECONNREFUSED');
  }
  return /ECONNREFUSED/i.test(err.message || '');
}

// OpenSSL legacy flags
const legacySecureOptions =
  (crypto.constants.SSL_OP_LEGACY_SERVER_CONNECT || 0x00000004) |
  (crypto.constants.SSL_OP_ALLOW_UNSAFE_LEGACY_RENEGOTIATION || 0x00040000) |
  (crypto.constants.SSL_OP_DONT_INSERT_EMPTY_FRAGMENTS || 0x00000800) |
  (crypto.constants.SSL_OP_ALL || 0x80000544);

const httpsAgent = new https.Agent({
  rejectUnauthorized: false,
  ciphers: 'DEFAULT:@SECLEVEL=0:ALL',
  secureOptions: legacySecureOptions,
  minVersion: 'TLSv1',
  maxVersion: 'TLSv1.3',
  checkServerIdentity: () => undefined,
  keepAlive: true,
  maxSockets: 4,
  timeout: 10000,
});

const httpAgent = new http.Agent({
  keepAlive: true,
  maxSockets: 4,
  timeout: 10000,
});

function getRouterScheme(plainHttp = usePlainHttp) {
  return plainHttp ? 'http' : 'https';
}

function getRouterAuthority(plainHttp = usePlainHttp, port = activeRouterPort) {
  const isDefaultPort = (plainHttp && port === 80) || (!plainHttp && port === 443);
  return isDefaultPort ? ROUTER_IP : `${ROUTER_IP}:${port}`;
}

// In-memory Cookie Jar pre-seeded with Huawei menu/language defaults
const cookieJar = new Map(HUAWEI_INITIAL_COOKIES);

function getMergedCookieHeader(incomingCookieHeader) {
  const cookies = new Map(cookieJar);

  // Merge incoming from browser
  if (incomingCookieHeader) {
    for (const part of incomingCookieHeader.split(';')) {
      const idx = part.indexOf('=');
      if (idx > 0) {
        const k = part.slice(0, idx).trim();
        const v = part.slice(idx + 1).trim();
        if (k) cookies.set(k, v);
      }
    }
  }

  return Array.from(cookies.entries())
    .map(([k, v]) => `${k}=${v}`)
    .join('; ');
}

function saveCookiesFromResponse(setCookieHeaders) {
  if (!setCookieHeaders) return;
  const list = Array.isArray(setCookieHeaders) ? setCookieHeaders : [setCookieHeaders];
  for (const str of list) {
    const main = str.split(';')[0];
    const idx = main.indexOf('=');
    if (idx > 0) {
      const key = main.slice(0, idx).trim();
      const val = main.slice(idx + 1).trim();
      if (!key) continue;
      if (!val || val.toLowerCase() === 'deleted') {
        cookieJar.delete(key);
      } else {
        cookieJar.set(key, val);
        console.log(`[Proxy 🍪 Session Cookie Saved] ${key}=${val}`);
      }
    }
  }
}

function fetchFromRouter(targetUrl, method, incomingHeaders, reqBody, retries = 2) {
  return new Promise((resolve, reject) => {
    const parsed = new URL(targetUrl);
    const requestUsePlainHttp = parsed.protocol === 'http:';
    const requestPort = parsed.port ? parseInt(parsed.port, 10) : (requestUsePlainHttp ? 80 : 443);
    const scheme = getRouterScheme(requestUsePlainHttp);
    const routerAuthority = getRouterAuthority(requestUsePlainHttp, requestPort);
    const incomingReferer = incomingHeaders.referer || '';
    const rewrittenReferer = incomingReferer
      ? incomingReferer.replace(
          /https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?/gi,
          `${scheme}://${routerAuthority}`
        )
      : `${scheme}://${routerAuthority}/`;

    const headers = {
      ...incomingHeaders,
      host: routerAuthority,
      referer: rewrittenReferer,
      origin: `${scheme}://${routerAuthority}`,
      'accept-encoding': 'identity',
    };
    delete headers['connection'];
    delete headers['keep-alive'];
    delete headers['proxy-connection'];

    // Inject all accumulated session cookies
    const mergedCookies = getMergedCookieHeader(incomingHeaders.cookie);
    if (mergedCookies) {
      headers.cookie = mergedCookies;
    }

    if (reqBody && reqBody.length > 0) {
      headers['content-length'] = reqBody.length;
    } else if (method === 'POST' || method === 'PUT') {
      headers['content-length'] = 0;
    } else {
      delete headers['content-length'];
    }

    const options = {
      protocol: `${scheme}:`,
      hostname: ROUTER_IP.replace(/^\[|\]$/g, ''),
      port: requestPort,
      path: parsed.pathname + parsed.search,
      method: method,
      headers: headers,
      agent: requestUsePlainHttp ? httpAgent : httpsAgent,
      insecureHTTPParser: true,
      timeout: 10000,
    };

    console.log(`[Proxy 📡] ${options.method} ${scheme}://${ROUTER_IP}:${options.port}${options.path}`);

    const transport = requestUsePlainHttp ? http : https;
    const req = transport.request(options, (res) => {
      if (!HAS_EXPLICIT_PORT_OR_SCHEME && requestUsePlainHttp && requestPort === 80 && !usePlainHttp) {
        usePlainHttp = true;
        activeRouterPort = 80;
      }

      // Save any session cookies returned by the router
      if (res.headers['set-cookie']) {
        saveCookiesFromResponse(res.headers['set-cookie']);
      }

      if ([301, 302, 303, 307, 308].includes(res.statusCode) && res.headers.location) {
        console.log(`[Proxy 🔄 Redirect] Code ${res.statusCode} -> ${res.headers.location}`);
      }

      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => {
        let buffer = Buffer.concat(chunks);
        let decompressed = false;
        const encoding = res.headers['content-encoding'];
        try {
          if (encoding === 'gzip') {
            buffer = zlib.gunzipSync(buffer);
            decompressed = true;
          } else if (encoding === 'deflate') {
            buffer = zlib.inflateSync(buffer);
            decompressed = true;
          }
        } catch {}

        resolve({
          statusCode: res.statusCode,
          headers: res.headers,
          buffer,
          decompressed,
        });
      });
    });

    req.on('timeout', () => {
      req.destroy(new Error('Router request timed out'));
    });

    req.on('error', (err) => {
      if (
        isConnectionRefusedError(err) &&
        !HAS_EXPLICIT_PORT_OR_SCHEME &&
        !requestUsePlainHttp &&
        requestPort === 443
      ) {
        console.warn(
          `[Proxy 🔄 Fallback] HTTPS on ${ROUTER_IP}:443 refused connection (ECONNREFUSED); falling back to plain HTTP on port 80...`
        );
        const fallbackUrl = `http://${getRouterAuthority(true, 80)}${parsed.pathname}${parsed.search}`;
        fetchFromRouter(fallbackUrl, method, incomingHeaders, reqBody, retries)
          .then(resolve)
          .catch(reject);
        return;
      }

      const isRetryable =
        err.code === 'ECONNRESET' ||
        err.code === 'ETIMEDOUT' ||
        err.message.includes('socket hang up') ||
        err.message.includes('timed out');
      if (retries > 0 && isRetryable) {
        console.warn(`[Proxy ⚠️ Retry ${3 - retries}/2] ${err.message} on ${options.path}, retrying...`);
        setTimeout(() => {
          fetchFromRouter(targetUrl, method, incomingHeaders, reqBody, retries - 1)
            .then(resolve)
            .catch(reject);
        }, 200);
        return;
      }
      reject(err);
    });

    if (reqBody && reqBody.length > 0) {
      req.write(reqBody);
    }
    req.end();
  });
}

const server = http.createServer(async (clientReq, clientRes) => {
  console.log(`\n[Proxy 📥] ${clientReq.method} ${clientReq.url}`);

  const clientChunks = [];
  clientReq.on('data', (chunk) => clientChunks.push(chunk));
  clientReq.on('end', async () => {
    const clientBody = Buffer.concat(clientChunks);

    try {
      const requestPath = clientReq.url.startsWith('/') ? clientReq.url : `/${clientReq.url}`;
      const targetUrl = `${getRouterScheme()}://${getRouterAuthority()}${requestPath}`;
      const routerRes = await fetchFromRouter(
        targetUrl,
        clientReq.method,
        clientReq.headers,
        clientBody
      );

      const headers = { ...routerRes.headers };
      const escapedIp = escapeRegExp(ROUTER_IP);
      console.log(`[Proxy 📤] Responded: ${routerRes.statusCode} (${headers['content-type'] || 'unknown'})`);

      // 1. Rewrite redirect Location headers to localhost
      if (headers.location) {
        headers.location = headers.location
          .replace(new RegExp(`https?://${escapedIp}(:\\d+)?`, 'gi'), `http://localhost:${activeProxyPort}`)
          .replace(new RegExp(`^//${escapedIp}(:\\d+)?`, 'gi'), `http://localhost:${activeProxyPort}`);
      }

      // 2. Format Set-Cookie for localhost
      if (headers['set-cookie']) {
        headers['set-cookie'] = (Array.isArray(headers['set-cookie']) ? headers['set-cookie'] : [headers['set-cookie']]).map((cookie) =>
          cookie
            .replace(/;\s*domain=[^;]+/gi, '')
            .replace(/;\s*secure/gi, '')
            .replace(/;\s*SameSite=None/gi, '; SameSite=Lax')
        );
      }

      delete headers['strict-transport-security'];
      delete headers['content-security-policy'];
      delete headers['content-security-policy-report-only'];
      delete headers['x-frame-options'];
      delete headers['x-content-type-options'];
      delete headers['transfer-encoding'];
      headers['x-legacy-router-proxy'] = '1';

      const contentType = headers['content-type'] || '';
      const isText = /html|javascript|json|text|xml/i.test(contentType) || !contentType;

      if (isText && routerRes.buffer.length > 0) {
        let bodyStr = routerRes.buffer.toString('utf-8');

        // Rewrite hardcoded router IP / HTTPS strings
        bodyStr = bodyStr
          .replace(new RegExp(`https?:\\/\\/${escapedIp}(:\\d+)?`, 'gi'), `http://localhost:${activeProxyPort}`)
          .replace(new RegExp(`https?:\\\\/\\\\/${escapedIp}(:\\d+)?`, 'gi'), `http:\\/\\/localhost:${activeProxyPort}`)
          .replace(new RegExp(`(?<=['"(])//${escapedIp}(:\\d+)?`, 'gi'), `//localhost:${activeProxyPort}`)
          .replace(new RegExp(`(['"])${escapedIp}(:\\d+)?\\1`, 'g'), `$1localhost:${activeProxyPort}$1`);

        const modifiedBuffer = Buffer.from(bodyStr, 'utf-8');
        headers['content-length'] = modifiedBuffer.length;
        delete headers['content-encoding'];

        clientRes.writeHead(routerRes.statusCode, headers);
        clientRes.end(modifiedBuffer);
      } else {
        if (routerRes.decompressed) {
          delete headers['content-encoding'];
          headers['content-length'] = routerRes.buffer.length;
        }
        clientRes.writeHead(routerRes.statusCode, headers);
        clientRes.end(routerRes.buffer);
      }
    } catch (err) {
      const errorMsg = err.message || err.code || String(err);
      console.error(`[Proxy ❌ Error]`, errorMsg);
      clientRes.writeHead(502, {
        'Content-Type': 'text/html; charset=utf-8',
        'X-Legacy-Router-Proxy': '1',
      });
      clientRes.end(`
        <div style="font-family: sans-serif; padding: 2rem; max-width: 500px; margin: 40px auto; border: 1px solid #f87171; border-radius: 8px; background: #fef2f2;">
          <h3 style="color: #b91c1c; margin-top:0;">Router Connection Error</h3>
          <p>${errorMsg}</p>
          <button onclick="location.reload()" style="padding: 6px 12px; background: #2563eb; color: #fff; border: none; border-radius: 4px; cursor: pointer;">Retry</button>
        </div>
      `);
    }
  });
});

function isExistingRouterProxy(port) {
  return new Promise((resolve) => {
    const req = http.get(
      {
        hostname: '127.0.0.1',
        port,
        path: '/html/ajaxref/updatachallangeajax.asp',
        timeout: 3500,
      },
      (res) => {
        const chunks = [];
        res.on('data', (chunk) => chunks.push(chunk));
        res.on('end', () => {
          const body = Buffer.concat(chunks).toString('utf-8').trim();
          const hasProxyHeader = res.headers['x-legacy-router-proxy'] === '1';
          const isProxyError = /Legacy Router Proxy Error|Router Connection Error/i.test(body);
          const isRouterRedirect =
            [301, 302, 303, 307, 308].includes(res.statusCode) &&
            new RegExp(`localhost:${port}|/html/`, 'i').test(res.headers.location || '');
          const unquoted = body.replace(/^["']|["']$/g, '').trim();
          const isChallengeToken =
            res.statusCode === 200 && /^[0-9a-zA-Z_-]{8,128}$/.test(unquoted);
          const hasRouterMarkers =
            /txt_Username|SubmitFormWithChallange|updatachallange|frameset/i.test(body) ||
            new RegExp(`localhost:${port}`, 'i').test(body);

          resolve(
            hasProxyHeader ||
              isProxyError ||
              isRouterRedirect ||
              isChallengeToken ||
              hasRouterMarkers
          );
        });
      }
    );

    req.on('timeout', () => {
      req.destroy();
      resolve(false);
    });
    req.on('error', () => resolve(false));
  });
}

function tryListen(port) {
  return new Promise((resolve) => {
    const onError = (err) => {
      server.removeListener('listening', onListening);
      resolve({ ok: false, err });
    };
    const onListening = () => {
      server.removeListener('error', onError);
      resolve({ ok: true });
    };
    server.once('error', onError);
    server.once('listening', onListening);
    server.listen(port);
  });
}

async function startServerWithFallback() {
  const maxAttempts = 10;
  for (let offset = 0; offset < maxAttempts; offset++) {
    const candidatePort = DEFAULT_PROXY_PORT + offset;
    const result = await tryListen(candidatePort);
    if (result.ok) {
      const addr = server.address();
      activeProxyPort = typeof addr === 'object' && addr ? addr.port : candidatePort;
      console.log(`\n======================================================`);
      console.log(`🚀 Legacy Router & Hardware Proxy with Active Session Cookie Jar!`);
      console.log(`🎯 Target Router: ${getRouterScheme()}://${ROUTER_IP}:${activeRouterPort}${HAS_EXPLICIT_PORT_OR_SCHEME ? '' : ' (auto-fallback to HTTP :80 if refused)'}`);
      console.log(`👉 Open: http://localhost:${activeProxyPort}`);
      console.log(`🍪 Server-side session management active`);
      console.log(`======================================================\n`);
      return;
    }

    if (result.err.code !== 'EADDRINUSE') {
      throw result.err;
    }

    if (offset === 0) {
      const alreadyProxy = await isExistingRouterProxy(candidatePort);
      if (alreadyProxy) {
        console.log(`\n======================================================`);
        console.log(`ℹ️  Port ${candidatePort} is already serving the Router Proxy`);
        console.log(`   (e.g., Mr. House or another instance of this proxy).`);
        console.log(`👉 Open directly: http://localhost:${candidatePort}`);
        console.log(`🔒 Reusing it avoids Huawei "Duplicated Login" session`);
        console.log(`   conflicts and extra TLS load on the router.`);
        console.log(`======================================================\n`);
        process.exit(0);
      }
      console.warn(
        `[Proxy ⚠️] Port ${candidatePort} is in use by another app; trying fallback port...`
      );
    } else {
      console.warn(`[Proxy ⚠️] Port ${candidatePort} is also in use, trying next port...`);
    }
  }

  throw new Error(
    `No available proxy port found in range ${DEFAULT_PROXY_PORT}-${DEFAULT_PROXY_PORT + maxAttempts - 1}`
  );
}

startServerWithFallback().catch((err) => {
  console.error(`[Proxy ❌ Startup Error]`, err.message);
  process.exit(1);
});
