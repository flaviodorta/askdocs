import DocumentsPanel from "@/components/DocumentsPanel";

export default function DocumentsPage() {
  return (
    <section>
      <h1>Documentos</h1>
      <p className="lead">
        Envie um PDF, texto ou markdown. Quando o processamento terminar, faça
        perguntas na aba Chat — as respostas sempre citam a fonte.
      </p>
      <DocumentsPanel />
    </section>
  );
}
