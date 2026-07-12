"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { ApiError, login, register } from "@/lib/api";

export default function AuthForm() {
  const router = useRouter();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      if (mode === "login") {
        await login(email, password);
      } else {
        await register(email, password);
      }
      router.push("/");
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : "Não foi possível falar com o servidor. Ele está rodando?",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="card auth-form" onSubmit={handleSubmit}>
      <label>
        E-mail
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          autoComplete="email"
          required
        />
      </label>
      <label>
        Senha {mode === "register" && <small>(mínimo 8 caracteres)</small>}
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete={mode === "login" ? "current-password" : "new-password"}
          minLength={mode === "register" ? 8 : undefined}
          required
        />
      </label>

      {error && <p className="error-banner">{error}</p>}

      <button className="primary" type="submit" disabled={busy}>
        {busy ? "Aguarde…" : mode === "login" ? "Entrar" : "Criar conta"}
      </button>

      <button
        type="button"
        className="link-button"
        onClick={() => setMode(mode === "login" ? "register" : "login")}
      >
        {mode === "login"
          ? "Não tem conta? Criar uma"
          : "Já tem conta? Entrar"}
      </button>
    </form>
  );
}
