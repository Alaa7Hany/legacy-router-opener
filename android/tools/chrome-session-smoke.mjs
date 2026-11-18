import { execFile, execFileSync, spawn } from 'node:child_process';
import { mkdtemp, readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { promisify } from 'node:util';

const [serial, hostChromeExecutable] = process.argv.slice(2);
if (!serial || !hostChromeExecutable) {
    throw new Error('Usage: node android/tools/chrome-session-smoke.mjs <adb-serial> <host-chrome-executable>');
}
const app = 'com.alaa.legacyrouteropener';
const runId = Date.now().toString(36);
const fixtureA = 'LRO fixture A ' + runId;
const fixtureB = 'LRO fixture B ' + runId;
const exec = promisify(execFile);

function adb(...args) {
    return execFileSync('adb', ['-s', serial, ...args], { encoding: 'utf8', timeout: 15000 });
}

function launchOpener() {
    adb('shell', 'am', 'start', '-f', '0x04000000', '-n', app + '/.presentation.MainActivity');
}

async function tree() {
    const { stdout } = await exec('android', ['layout', '--device', serial],
        { encoding: 'utf8', timeout: 15000, maxBuffer: 2 * 1024 * 1024 });
    const windows = JSON.parse(stdout);
    function flatten(nodes, packageName) {
        return (nodes ?? []).flatMap(node => [{
            ...node,
            package: node['resource-id']?.split(':')[0] || packageName,
            focused: node.state?.includes('FOCUSED'),
        }, ...flatten(node.children, packageName)]);
    }
    return windows.filter(window => window.type === 'APPLICATION')
        .flatMap(window => flatten(window.content, window['window-title']?.split('/')[0]));
}

async function find(predicate, description) {
    const end = Date.now() + 20000;
    let nodes;
    do {
        nodes = await tree();
        const result = nodes.find(predicate);
        if (result) return result;
        await delay(300);
    } while (Date.now() < end);
    const markers = nodes.map(node => node.text).filter(text =>
        /^(LRO fixture|ERR_|502|Couldn't open|Couldn’t open|Submit stale form|This site can)/.test(text || ''));
    throw new Error('Could not find ' + description + '. Visible test markers: ' + (markers.join(', ') || 'none'));
}

function label(value) {
    return node => node.text === value || node['content-desc'] === value;
}

function tap(node) {
    const bounds = node.bounds.match(/\d+/g).map(Number);
    adb('shell', 'input', 'tap', String(Math.floor((bounds[0] + bounds[2]) / 2)),
        String(Math.floor((bounds[1] + bounds[3]) / 2)));
}

async function addressInput() {
    const isInput = node => node.class === 'android.widget.EditText' && !node['resource-id']?.endsWith(':id/url_bar');
    const input = (await tree()).find(isInput);
    if (input) return input;
    tap(await find(label('Change device'), 'Change device'));
    return find(isInput, 'the target address field');
}

async function setTarget(target) {
    tap(await addressInput());
    await find(node => node.class === 'android.widget.EditText' && node.focused, 'the focused address field');
    adb('shell', 'input', 'keycombination', '113', '29'); // Ctrl+A
    adb('shell', 'input', 'text', target);
    await find(label(target), 'the entered address');
    adb('shell', 'input', 'keyevent', '4');
}

async function openSession(expectedTitle) {
    const node = await find(label('Open router page'), 'the in-app opener');
    tap(node);
    await find(label(expectedTitle), 'the active fixture in WebView');
    // The fixture includes an absolute URL to itself. The proxy rewrites it to its current
    // session URL, which lets the harness launch the exact origin in Chrome.
    const session = await find(n => n.text?.startsWith('Fixture session: '), 'the rewritten session URL');
    const authority = session.text.match(/[a-f\d]{32}\.localhost:\d+/)?.[0];
    if (!authority) throw new Error('The fixture URL was not rewritten to the session hostname.');
    return authority;
}

async function fixture(title, handler) {
    const server = createServer(handler);
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
    const { port } = server.address();
    const name = 'tcp:' + port;
    adb('reverse', name, name);
    return {
        url: 'http://127.0.0.1:' + port,
        close() {
            adb('reverse', '--remove', name);
            server.closeAllConnections();
            server.close();
        },
        title,
    };
}

function html(title, body) {
    return '<!doctype html><meta name="viewport" content="width=device-width"><title>' + title +
        '</title><h1>' + title + '</h1>' + body;
}

async function waitFor(read, ready, description) {
    const end = Date.now() + 15000;
    do {
        const value = await read();
        if (ready(value)) return value;
        await delay(250);
    } while (Date.now() < end);
    throw new Error('Timed out waiting for ' + description);
}

async function startHostChrome() {
    const profile = await mkdtemp(join(tmpdir(), 'router-chrome-'));
    const browser = spawn(hostChromeExecutable, [
        '--headless', '--no-first-run', '--no-default-browser-check', '--remote-debugging-port=0',
        '--user-data-dir=' + profile, 'about:blank',
    ], { windowsHide: true, stdio: 'ignore' });
    const forwards = new Set();
    const connection = await waitFor(
        () => readFile(join(profile, 'DevToolsActivePort'), 'utf8').catch(() => ''),
        value => /^\d+/.test(value), 'Chrome DevTools');
    const devtools = 'http://127.0.0.1:' + connection.split('\n')[0];
    return {
        async open(authority, marker) {
            const port = authority.split(':')[1];
            if (!forwards.has(port)) {
                const route = 'tcp:' + port;
                adb('forward', '--no-rebind', route, route);
                forwards.add(port);
            }
            const target = await fetch(devtools + '/json/new?about:blank', { method: 'PUT' }).then(r => r.json());
            const socket = new WebSocket(target.webSocketDebuggerUrl);
            await new Promise((resolve, reject) => {
                socket.addEventListener('open', resolve, { once: true });
                socket.addEventListener('error', reject, { once: true });
            });
            let sequence = 0;
            function call(method, params) {
                return new Promise((resolve, reject) => {
                    const id = ++sequence;
                    const timeout = setTimeout(() => {
                        socket.removeEventListener('message', receive);
                        reject(new Error('No response for ' + method));
                    }, 5000);
                    function receive(event) {
                        const response = JSON.parse(event.data);
                        if (response.id !== id) return;
                        clearTimeout(timeout);
                        socket.removeEventListener('message', receive);
                        if (response.error) reject(new Error(response.error.message));
                        else resolve(response.result);
                    }
                    socket.addEventListener('message', receive);
                    socket.send(JSON.stringify({ id, method, params }));
                });
            }
            const evaluate = async expression =>
                (await call('Runtime.evaluate', { expression, returnByValue: true })).result.value;
            await call('Page.navigate', { url: 'http://' + authority + '/' });
            await waitFor(() => evaluate('document.title').catch(() => ''), title => title === marker, marker);
            return {
                async submit() {
                    const clicked = await evaluate("(() => { const button = document.querySelector('form button'); " +
                        "if (!button) return false; button.click(); return true; })()");
                    if (!clicked) throw new Error('The retained Chrome tab did not contain the old form.');
                    await waitFor(() => evaluate('!!document.querySelector("form button")').catch(() => true),
                        present => present === false, 'the retired form navigation');
                },
                close() { socket.close(); },
            };
        },
        close() {
            browser.kill();
            forwards.forEach(port => adb('forward', '--remove', 'tcp:' + port));
        },
    };
}

let requestsToB = 0;
let requestsToA = 0;
let cookieLeak = false;
const a = await fixture(fixtureA, (req, res) => {
    requestsToA++;
    const markup = html(fixtureA, '<p>Fixture session: http://' + req.headers.host +
        '/</p><form method="POST" action="/submit"><button type="submit">Submit stale form</button></form>');
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Content-Length': Buffer.byteLength(markup),
        'Set-Cookie': 'old_device_session=A; Path=/' });
    res.end(markup);
});
const b = await fixture(fixtureB, (req, res) => {
    requestsToB++;
    cookieLeak ||= (req.headers.cookie ?? '').includes('old_device_session=');
    const markup = html(fixtureB, '<p>Fixture session: http://' + req.headers.host + '/</p>');
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Content-Length': Buffer.byteLength(markup) });
    res.end(markup);
});

let original;
let hostChrome;
let initialTab;
let replacementTab;
try {
    hostChrome = await startHostChrome();
    adb('shell', 'am', 'force-stop', app);
    launchOpener();
    original = (await addressInput()).text;
    await setTarget(a.url);
    const authorityA = await openSession(fixtureA);
    initialTab = await hostChrome.open(authorityA, fixtureA);

    adb('shell', 'am', 'force-stop', app);
    launchOpener();
    await setTarget(b.url);
    const authorityB = await openSession(fixtureB);
    replacementTab = await hostChrome.open(authorityB, fixtureB);
    if (!authorityA || !authorityB || authorityA === authorityB ||
        authorityA.split(':')[1] !== authorityB.split(':')[1]) {
        throw new Error('The restart did not produce two different session hosts on the same port.');
    }

    // Let the replacement tab finish automatic favicon and resource loads before measuring.
    await delay(1000);
    const beforeSubmit = requestsToB;
    await initialTab.submit();
    if (requestsToB !== beforeSubmit || cookieLeak) {
        throw new Error('Device B isolation failed. Additional requests=' + (requestsToB - beforeSubmit) +
            ', old-device cookie=' + cookieLeak);
    }
    console.log('Passed: Device B received no requests or cookies from the stale Chrome tab after restart.');
} catch (failure) {
    console.error('Fixture requests before failure. A=' + requestsToA + ', B=' + requestsToB);
    throw failure;
} finally {
    try {
        if (original && /^[A-Za-z0-9._:[\]/-]+$/.test(original)) {
            launchOpener();
            await setTarget(original);
            const apply = (await tree()).find(label('Apply changes'));
            if (apply) tap(apply);
        }
    } finally {
        initialTab?.close();
        replacementTab?.close();
        hostChrome?.close();
        a.close();
        b.close();
    }
}
