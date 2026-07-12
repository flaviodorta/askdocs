import AuthForm from "@/components/AuthForm";

export default function LoginPage() {
  return (
    <section>
      <h1>Entrar</h1>
      <p className="lead">
        Seus documentos e conversas são privados — cada conta vê apenas o que é
        seu.
      </p>
      <AuthForm />
    </section>
  );
}
