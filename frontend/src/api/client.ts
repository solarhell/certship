export class ApiError extends Error {
  code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

export async function rpc<TReq, TRes>(
  service: string,
  method: string,
  request: TReq,
): Promise<TRes> {
  const token = localStorage.getItem("token");
  const res = await fetch(`/certship.v1.${service}/${method}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(request),
  });

  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    const code = (body as Record<string, string>).code ?? "unknown";
    const message = (body as Record<string, string>).message ?? res.statusText;
    if (code === "unauthenticated") {
      localStorage.removeItem("token");
      window.location.href = "/login";
    }
    throw new ApiError(code, message);
  }

  return res.json() as Promise<TRes>;
}
