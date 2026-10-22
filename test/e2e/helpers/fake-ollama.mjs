// A minimal stand-in for Ollama's HTTP API, enough for an open_only chat turn
// to run end to end without a model. Every request is recorded so specs can
// assert on what the agent sent.
import { createServer } from "node:http";

export const FAKE_MODEL = "fake-model";

export async function startFakeOllama({ reply = "fake reply from the e2e model", chunkDelayMs = 20 } = {}) {
  const requests = [];
  const sockets = new Set();

  const server = createServer(async (req, res) => {
    const body = await readBody(req);
    let json = null;
    try {
      json = body ? JSON.parse(body) : null;
    } catch {
      // keep the raw body
    }
    requests.push({ method: req.method, path: req.url, body: json ?? body });

    switch (req.url.split("?")[0]) {
      case "/":
        return text(res, "Ollama is running");
      case "/api/version":
        return send(res, { version: "0.0.0-e2e" });
      case "/api/tags":
      case "/api/ps":
        return send(res, { models: [modelEntry()] });
      case "/api/show":
        return send(res, {
          modelfile: "",
          parameters: "",
          template: "{{ .Prompt }}",
          details: modelEntry().details,
          model_info: { "general.context_length": 8192 },
          capabilities: ["completion", "tools"],
        });
      case "/api/chat":
        return streamChat(res, json, reply, chunkDelayMs);
      case "/api/generate":
        return streamGenerate(res, reply, chunkDelayMs);
      case "/api/embed":
      case "/api/embeddings":
        return send(res, { embeddings: [new Array(8).fill(0.1)], embedding: new Array(8).fill(0.1) });
      default:
        res.writeHead(404, { "content-type": "application/json" });
        return res.end(JSON.stringify({ error: `fake-ollama: unhandled ${req.method} ${req.url}` }));
    }
  });
  server.on("connection", (s) => {
    sockets.add(s);
    s.on("close", () => sockets.delete(s));
  });

  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const { port } = server.address();

  return {
    url: `http://127.0.0.1:${port}`,
    requests,
    chatRequests: () => requests.filter((r) => r.path.startsWith("/api/chat")),
    close: () =>
      new Promise((r) => {
        for (const s of sockets) s.destroy();
        server.close(() => r());
      }),
  };
}

function modelEntry() {
  return {
    name: FAKE_MODEL,
    model: FAKE_MODEL,
    modified_at: "2026-01-01T00:00:00Z",
    size: 1,
    digest: "e2e",
    details: { format: "gguf", family: "e2e", parameter_size: "1B", quantization_level: "Q4_0" },
  };
}

async function streamChat(res, req, reply, delay) {
  res.writeHead(200, { "content-type": "application/x-ndjson" });
  const model = req?.model ?? FAKE_MODEL;
  for (const piece of chunks(reply)) {
    if (res.destroyed) return;
    res.write(JSON.stringify({ model, message: { role: "assistant", content: piece }, done: false }) + "\n");
    await sleep(delay);
  }
  res.end(
    JSON.stringify({
      model,
      message: { role: "assistant", content: "" },
      done: true,
      done_reason: "stop",
      prompt_eval_count: 10,
      eval_count: 10,
    }) + "\n",
  );
}

async function streamGenerate(res, reply, delay) {
  res.writeHead(200, { "content-type": "application/x-ndjson" });
  for (const piece of chunks(reply)) {
    if (res.destroyed) return;
    res.write(JSON.stringify({ model: FAKE_MODEL, response: piece, done: false }) + "\n");
    await sleep(delay);
  }
  res.end(JSON.stringify({ model: FAKE_MODEL, response: "", done: true }) + "\n");
}

// chunks splits on word boundaries so the reply streams like a real model.
function chunks(s) {
  return s.match(/\S+\s*/g) ?? [s];
}

function send(res, obj) {
  res.writeHead(200, { "content-type": "application/json" });
  res.end(JSON.stringify(obj));
}

function text(res, s) {
  res.writeHead(200, { "content-type": "text/plain" });
  res.end(s);
}

function readBody(req) {
  return new Promise((resolve) => {
    let data = "";
    req.on("data", (c) => (data += c));
    req.on("end", () => resolve(data));
  });
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
