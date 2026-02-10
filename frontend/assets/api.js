export async function api(path, method = "GET", body) {
  const opts = { method, headers: {} };

  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }

  const res = await fetch("/api" + path, opts);

  // пробуем распарсить JSON всегда
  let data = null;
  const text = await res.text();
  try { data = text ? JSON.parse(text) : null; } catch { data = null; }

  if (!res.ok) {
    const msg = (data && (data.error || data.message)) ? (data.error || data.message) : (text || ("HTTP " + res.status));
    throw new Error(msg);
  }

  return data;
}
