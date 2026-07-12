"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  ApiError,
  listDocuments,
  retryDocument,
  uploadDocument,
  type DocumentInfo,
} from "@/lib/api";

const STATUS_LABEL: Record<DocumentInfo["status"], string> = {
  queued: "na fila",
  processing: "processando",
  ready: "pronto",
  failed: "falhou",
};

const POLL_MS = 2500;

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  return "Não foi possível falar com o servidor. Ele está rodando?";
}

export default function DocumentsPanel() {
  const [docs, setDocs] = useState<DocumentInfo[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const hasPending = useRef(false);

  const refresh = useCallback(async () => {
    try {
      const list = await listDocuments();
      hasPending.current = list.some(
        (d) => d.status === "queued" || d.status === "processing",
      );
      setDocs(list);
      setError(null);
    } catch (err) {
      setError(errorMessage(err));
    }
  }, []);

  // Load once on mount, then poll while any document is still being
  // ingested, so status badges flip from "na fila" to "pronto" without a
  // manual refresh. The initial load runs on a zero timer so no state update
  // happens synchronously inside the effect body.
  useEffect(() => {
    const initial = setTimeout(refresh, 0);
    const timer = setInterval(() => {
      if (hasPending.current) refresh();
    }, POLL_MS);
    return () => {
      clearTimeout(initial);
      clearInterval(timer);
    };
  }, [refresh]);

  async function handleUpload(event: React.FormEvent) {
    event.preventDefault();
    const file = fileInput.current?.files?.[0];
    if (!file) return;

    setUploading(true);
    try {
      await uploadDocument(file);
      if (fileInput.current) fileInput.current.value = "";
      await refresh();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setUploading(false);
    }
  }

  async function handleRetry(id: string) {
    try {
      await retryDocument(id);
      await refresh();
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  return (
    <div>
      <form className="card upload-form" onSubmit={handleUpload}>
        <input
          ref={fileInput}
          type="file"
          accept=".pdf,.txt,.md,application/pdf,text/plain,text/markdown"
          required
        />
        <button className="primary" type="submit" disabled={uploading}>
          {uploading ? "Enviando…" : "Enviar"}
        </button>
      </form>

      {error && <p className="error-banner">{error}</p>}

      {docs && docs.length === 0 && (
        <p className="empty">Nenhum documento ainda. Envie o primeiro acima.</p>
      )}

      {docs && docs.length > 0 && (
        <ul className="doc-list">
          {docs.map((doc) => (
            <li className="card" key={doc.id}>
              <div className="doc-row">
                <span className="doc-name" title={doc.filename}>
                  {doc.filename}
                </span>
                <span className={`badge ${doc.status}`}>
                  {STATUS_LABEL[doc.status]}
                </span>
                {doc.status === "failed" && (
                  <button onClick={() => handleRetry(doc.id)}>
                    Tentar de novo
                  </button>
                )}
              </div>
              {doc.status === "failed" && doc.error && (
                <p className="doc-error">{doc.error}</p>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
