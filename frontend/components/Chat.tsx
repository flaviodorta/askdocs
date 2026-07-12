"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { ApiError, ask, type Citation } from "@/lib/api";

interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  citations: Citation[];
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  return "Não foi possível falar com o servidor. Ele está rodando?";
}

// Sources are the core of the UX: every assistant answer renders the chunks
// that grounded it, expandable to the snippet (CLAUDE.md: requirement, not
// decoration).
function CitationList({ citations }: { citations: Citation[] }) {
  if (citations.length === 0) return null;
  return (
    <div className="citations">
      <span className="citations-title">
        {citations.length === 1 ? "Fonte" : "Fontes"}
      </span>
      {citations.map((citation) => (
        <details className="citation" key={citation.chunk_id}>
          <summary>{citation.filename}</summary>
          <p className="citation-snippet">{citation.snippet}</p>
        </details>
      ))}
    </div>
  );
}

export default function Chat() {
  const router = useRouter();
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [conversationId, setConversationId] = useState<string | undefined>();
  const [question, setQuestion] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSend(event: React.FormEvent) {
    event.preventDefault();
    const trimmed = question.trim();
    if (!trimmed || sending) return;

    setMessages((prev) => [
      ...prev,
      { id: `local-${prev.length}`, role: "user", content: trimmed, citations: [] },
    ]);
    setQuestion("");
    setSending(true);
    setError(null);

    try {
      const result = await ask(trimmed, conversationId);
      setConversationId(result.conversation_id);
      setMessages((prev) => [
        ...prev,
        {
          id: result.message_id,
          role: "assistant",
          content: result.answer,
          citations: result.citations,
        },
      ]);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        router.push("/login");
        return;
      }
      setError(errorMessage(err));
    } finally {
      setSending(false);
    }
  }

  return (
    <div>
      <div className="chat">
        {messages.length === 0 && (
          <p className="empty">
            Nada por aqui ainda. Envie um documento na aba Documentos e faça a
            primeira pergunta.
          </p>
        )}
        {messages.map((msg) => (
          <div className={`msg ${msg.role}`} key={msg.id}>
            {msg.content}
            {msg.role === "assistant" && (
              <CitationList citations={msg.citations} />
            )}
          </div>
        ))}
        {sending && <p className="thinking">Consultando os documentos…</p>}
      </div>

      {error && <p className="error-banner">{error}</p>}

      <form className="chat-form" onSubmit={handleSend}>
        <input
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          placeholder="Qual o prazo de rescisão do contrato?"
          aria-label="Pergunta"
          disabled={sending}
        />
        <button className="primary" type="submit" disabled={sending || !question.trim()}>
          Perguntar
        </button>
      </form>
    </div>
  );
}
