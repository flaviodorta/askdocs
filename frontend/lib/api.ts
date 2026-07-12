// Typed client for the Go backend. The only place URLs and response shapes
// live — components import from here, never call fetch directly.

export type DocumentStatus = "queued" | "processing" | "ready" | "failed";

export interface DocumentInfo {
  id: string;
  filename: string;
  content_type: string;
  status: DocumentStatus;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface Citation {
  chunk_id: string;
  document_id: string;
  filename: string;
  snippet: string;
}

export interface AskResponse {
  conversation_id: string;
  answer: string;
  citations: Citation[];
  message_id: string;
  created_at: string;
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api${path}`, init);
  if (!res.ok) {
    let message = `A requisição falhou (${res.status}).`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      // non-JSON error body — keep the generic message
    }
    throw new ApiError(message, res.status);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

export interface UserInfo {
  id: string;
  email: string;
}

export function register(email: string, password: string): Promise<UserInfo> {
  return request<UserInfo>("/auth/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
}

export function login(email: string, password: string): Promise<UserInfo> {
  return request<UserInfo>("/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
}

export function logout(): Promise<void> {
  return request<void>("/auth/logout", { method: "POST" });
}

export function me(): Promise<UserInfo> {
  return request<UserInfo>("/auth/me");
}

export function listDocuments(): Promise<DocumentInfo[]> {
  return request<DocumentInfo[]>("/documents");
}

export function uploadDocument(file: File): Promise<DocumentInfo> {
  const form = new FormData();
  form.append("file", file);
  return request<DocumentInfo>("/documents", { method: "POST", body: form });
}

export function retryDocument(id: string): Promise<DocumentInfo> {
  return request<DocumentInfo>(`/documents/${encodeURIComponent(id)}/retry`, {
    method: "POST",
  });
}

export function ask(
  question: string,
  conversationId?: string,
): Promise<AskResponse> {
  return request<AskResponse>("/queries", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ question, conversation_id: conversationId }),
  });
}
