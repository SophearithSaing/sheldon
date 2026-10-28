import { isIP } from 'node:net'
import type { IncomingMessage } from 'node:http'
import type { Plugin, ProxyOptions, ViteDevServer } from 'vite'

export function loopbackOrigin(value: string): URL {
  let url: URL
  try {
    url = new URL(value)
  } catch {
    throw new Error('Development URLs must be canonical numeric-loopback HTTP origins with a port.')
  }
  const host = url.hostname.replace(/^\[|\]$/g, '')
  const loopback = (isIP(host) === 4 && host.startsWith('127.')) || host === '::1'
  if (
    !loopback ||
    url.protocol !== 'http:' ||
    !url.port ||
    Number(url.port) < 1 ||
    value !== url.origin
  ) {
    throw new Error('Development URLs must be canonical numeric-loopback HTTP origins with a port.')
  }
  return url
}

function serverOrigin(server: ViteDevServer): string {
  const address = server.httpServer?.address()
  if (!address || typeof address === 'string') return ''
  const host = isIP(address.address) === 6 ? `[${address.address}]` : address.address
  return `http://${host}:${address.port}`
}

function requestStatus(request: IncomingMessage, origin: string): number | undefined {
  const sensitive = new Set([
    'host',
    'origin',
    'authorization',
    'sec-fetch-site',
    'x-sheldon-challenge',
  ])
  const seen = new Set<string>()
  for (let index = 0; index < request.rawHeaders.length; index += 2) {
    const name = request.rawHeaders[index]!.toLowerCase()
    if (sensitive.has(name) && seen.has(name)) return 400
    seen.add(name)
  }
  if (!origin || request.headers.host !== origin.slice('http://'.length)) return 403
  if (!request.url?.startsWith('/') || request.url.startsWith('//')) return 403
  if (request.headers.origin !== undefined && request.headers.origin !== origin) return 403
  const site = request.headers['sec-fetch-site']
  if (site !== undefined && site !== 'same-origin' && site !== 'none') return 403
  const url = new URL(request.url, origin)
  let path: string
  try {
    path = decodeURIComponent(url.pathname).replaceAll('\\', '/')
  } catch {
    return 400
  }
  const filename = path
    .split('/')
    .filter(Boolean)
    .at(-1)
    ?.split(':')[0]
    ?.replace(/[ .]+$/, '')
    .toLowerCase()
  if (filename === 'instance.lock') return 403
  if (
    url.pathname === '/instance-proof' ||
    url.pathname === '/api' ||
    url.pathname.startsWith('/api/')
  ) {
    for (const name of ['token', 'accessToken', 'access_token']) {
      if (url.searchParams.has(name)) return 401
    }
  }
}

// Validate the browser-facing authority before changeOrigin rewrites Host for Go.
// Forwarded headers never establish trust. The same rules protect HMR upgrades.
export function protectedDevelopmentServer(): Plugin {
  return {
    name: 'sheldon-protected-development-server',
    configureServer(server) {
      const host = server.config.server.host
      if (typeof host !== 'string') throw new Error('Vite must bind a numeric loopback address.')
      const authority = isIP(host) === 6 ? `[${host}]` : host
      loopbackOrigin(`http://${authority}:${server.config.server.port}`)
      server.middlewares.use((request, response, next) => {
        const origin = serverOrigin(server)
        response.setHeader('Referrer-Policy', 'no-referrer')
        response.setHeader('X-Content-Type-Options', 'nosniff')
        response.setHeader('Cross-Origin-Resource-Policy', 'same-origin')
        response.setHeader('X-Frame-Options', 'DENY')
        response.setHeader('Cache-Control', 'no-store')
        response.setHeader(
          'Content-Security-Policy',
          `default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; connect-src 'self' ${origin.replace('http:', 'ws:')}; base-uri 'none'; frame-ancestors 'none'; form-action 'none'`,
        )
        const status = requestStatus(request, origin)
        if (status) {
          response.writeHead(status, { 'Content-Type': 'application/json' })
          response.end(JSON.stringify({ error: 'untrusted development request' }))
          return
        }
        next()
      })
      server.httpServer?.prependListener('upgrade', (request, socket) => {
        const status = requestStatus(request, serverOrigin(server))
        if (status) {
          socket.end(`HTTP/1.1 ${status} Forbidden\r\nConnection: close\r\n\r\n`, () =>
            socket.destroy(),
          )
        }
      })
    },
  }
}

export function backendProxy(target: string): ProxyOptions {
  loopbackOrigin(target)
  return {
    target,
    changeOrigin: true,
    ws: false,
    configure(proxy) {
      proxy.on('proxyReq', (request, incoming) => {
        // Same-origin browser GETs may omit Origin. The incoming authority was
        // checked by our middleware; never replace a supplied browser Origin.
        if (incoming.headers.origin === undefined) {
          request.setHeader('Origin', `http://${incoming.headers.host}`)
        }
        for (const name of request.getHeaderNames()) {
          if (name === 'forwarded' || name.startsWith('x-forwarded-')) request.removeHeader(name)
        }
      })
      // Pipe responses as they arrive, and release the upstream stream on disconnect.
      proxy.on('proxyRes', (upstream, _request, downstream) => {
        downstream.on('close', () => upstream.destroy())
      })
      proxy.on('error', (_error, request, response) => {
        // Vite's subsequent default error listener logs request.url. Never log query data.
        request.url = '/[redacted proxy request]'
        if ('writeHead' in response) {
          if (response.headersSent) {
            response.destroy()
            return
          }
          response.writeHead(502, { 'Content-Type': 'application/json' })
          response.end(JSON.stringify({ error: 'development backend unavailable' }))
        }
      })
    },
  }
}
