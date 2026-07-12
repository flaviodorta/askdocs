import Chat from "@/components/Chat";

export default function ChatPage() {
  return (
    <section>
      <h1>Chat</h1>
      <p className="lead">
        Pergunte em linguagem natural. Cada resposta mostra os trechos dos
        documentos que a embasaram.
      </p>
      <Chat />
    </section>
  );
}
