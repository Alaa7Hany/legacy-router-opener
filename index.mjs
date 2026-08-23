import http from 'node:http';
import https from 'node:https';
import crypto from 'node:crypto';
import zlib from 'node:zlib';
import { URL } from 'node:url';

const ROUTER_IP = process.env.ROUTER_IP || '192.168.1.1';
const PROXY_PORT = parseInt(process.env.PROXY_PORT || '8080', 10);

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
  maxSockets: 6,
  timeout: 15000,
});

// In-memory Cookie Jar so authenticated router sessions NEVER drop
const cookieJar = new Map();

function getMergedCookieHeader(incomingCookieHeader) {
  const cookies = new Map();

  // Load from proxy jar
  for (const [k, v] of cookieJar.entries()) {
    cookies.set(k, v);
  }

  // Merge incoming from browser
  if (incomingCookieHeader) {
    incomingCookieHeader.split(';').forEach((part) => {
      const [k, ...v] = part.trim().split('=');
      if (k) cookies.set(k.trim(), v.join('='));
    });
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
    const [k, ...v] = main.split('=');
    if (k && v.length > 0) {
      cookieJar.set(k.trim(), v.join('='));
      console.log(`[Proxy 🍪 Session Cookie Saved] ${k.trim()}=${v.join('=')}`);
    }
  }
}

function fetchFromRouter(targetUrl, method, incomingHeaders, reqBody, redirectCount = 0) {
  return new Promise((resolve, reject) => {
    if (redirectCount > 5) {
      return reject(new Error('Too many redirects from router'));
    }

    const parsed = new URL(targetUrl);
    const headers = {
      ...incomingHeaders,
      host: parsed.host,
      referer: `https://${ROUTER_IP}/`,
      origin: `https://${ROUTER_IP}`,
      'accept-encoding': 'identity',
    };

    // Inject all accumulated session cookies
    const mergedCookies = getMergedCookieHeader(incomingHeaders.cookie);
    if (mergedCookies) {
      headers.cookie = mergedCookies;
    }

    if (reqBody && reqBody.length > 0) {
      headers['content-length'] = reqBody.length;
    }

    const options = {
      protocol: 'https:',
      hostname: ROUTER_IP,
      port: 443,
      path: parsed.pathname + parsed.search,
      method: method,
      headers: headers,
      agent: httpsAgent,
    };

    console.log(`[Proxy 📡] ${options.method} https://${options.hostname}:${options.port}${options.path}`);

    const req = https.request(options, (res) => {
      // Save any session cookies returned by the router
      if (res.headers['set-cookie']) {
        saveCookiesFromResponse(res.headers['set-cookie']);
      }

      // If the router sends a redirect (e.g. 302 to /html/content.asp)
      if ([301, 302, 303, 307, 308].includes(res.statusCode) && res.headers.location) {
        let nextLoc = res.headers.location;
        console.log(`[Proxy 🔄 Redirect] Code ${res.statusCode} -> ${nextLoc}`);

        if (!nextLoc.startsWith('http://') && !nextLoc.startsWith('https://')) {
          nextLoc = `https://${ROUTER_IP}${nextLoc.startsWith('/') ? '' : '/'}${nextLoc}`;
        }

        // Return redirect directly to browser so URL bar updates, or follow if internal
        // For router login flows, returning the redirect to browser is best
      }

      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => {
        let buffer = Buffer.concat(chunks);
        const encoding = res.headers['content-encoding'];
        try {
          if (encoding === 'gzip') buffer = zlib.gunzipSync(buffer);
          else if (encoding === 'deflate') buffer = zlib.inflateSync(buffer);
        } catch {}

        resolve({
          statusCode: res.statusCode,
          headers: res.headers,
          buffer,
        });
      });
    });

    req.on('error', (err) => {
      if (err.code === 'ECONNRESET' || err.message.includes('socket hang up')) {
        console.warn(`[Proxy ⚠️ Retry] Socket reset on ${options.path}, retrying...`);
        setTimeout(() => {
          fetchFromRouter(targetUrl, method, incomingHeaders, reqBody, redirectCount).then(resolve).catch(reject);
        }, 200);
      } else {
        reject(err);
      }
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
      const targetUrl = `https://${ROUTER_IP}${clientReq.url}`;
      const routerRes = await fetchFromRouter(
        targetUrl,
        clientReq.method,
        clientReq.headers,
        clientBody
      );

      const headers = { ...routerRes.headers };
      console.log(`[Proxy 📤] Responded: ${routerRes.statusCode} (${headers['content-type'] || 'unknown'})`);

      // 1. Rewrite redirect Location headers to localhost
      if (headers.location) {
        headers.location = headers.location.replace(
          new RegExp(`https?://${ROUTER_IP}(:\\d+)?`, 'gi'),
          `http://localhost:${PROXY_PORT}`
        );
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

      const contentType = headers['content-type'] || '';
      const isText = /html|javascript|json|text|xml/i.test(contentType) || !contentType;

      if (isText && routerRes.buffer.length > 0) {
        let bodyStr = routerRes.buffer.toString('utf-8');

        // Rewrite hardcoded router IP / HTTPS strings
        bodyStr = bodyStr
          .replace(new RegExp(`https?:\\/\\/${ROUTER_IP}(:\\d+)?`, 'gi'), `http://localhost:${PROXY_PORT}`)
          .replace(new RegExp(`https?:\\\\/\\\\/${ROUTER_IP}(:\\d+)?`, 'gi'), `http:\\/\\/localhost:${PROXY_PORT}`)
          .replace(new RegExp(`['"]${ROUTER_IP}['"]`, 'g'), `'localhost:${PROXY_PORT}'`);

        const modifiedBuffer = Buffer.from(bodyStr, 'utf-8');
        headers['content-length'] = modifiedBuffer.length;
        delete headers['content-encoding'];

        clientRes.writeHead(routerRes.statusCode, headers);
        clientRes.end(modifiedBuffer);
      } else {
        clientRes.writeHead(routerRes.statusCode, headers);
        clientRes.end(routerRes.buffer);
      }
    } catch (err) {
      console.error(`[Proxy ❌ Error]`, err.message);
      clientRes.writeHead(502, { 'Content-Type': 'text/html; charset=utf-8' });
      clientRes.end(`
        <div style="font-family: sans-serif; padding: 2rem; max-width: 500px; margin: 40px auto; border: 1px solid #f87171; border-radius: 8px; background: #fef2f2;">
          <h3 style="color: #b91c1c; margin-top:0;">Router Connection Error</h3>
          <p>${err.message}</p>
          <button onclick="location.reload()" style="padding: 6px 12px; background: #2563eb; color: #fff; border: none; border-radius: 4px; cursor: pointer;">Retry</button>
        </div>
      `);
    }
  });
});

server.listen(PROXY_PORT, () => {
  console.log(`\n======================================================`);
  console.log(`🚀 TE Data Router Proxy with Active Session Cookie Jar!`);
  console.log(`👉 Open: http://localhost:${PROXY_PORT}`);
  console.log(`🍪 Server-side session management active`);
  console.log(`======================================================\n`);
});
