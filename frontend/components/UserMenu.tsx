"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { logout, me, type UserInfo } from "@/lib/api";

export default function UserMenu() {
  const router = useRouter();
  const pathname = usePathname();
  const [user, setUser] = useState<UserInfo | null>(null);
  const [checked, setChecked] = useState(false);

  // Zero timer keeps the state update out of the effect's synchronous body
  // (react-hooks/set-state-in-effect). Re-check on navigation so the menu
  // reflects a fresh login.
  useEffect(() => {
    const timer = setTimeout(async () => {
      try {
        setUser(await me());
      } catch {
        setUser(null);
      } finally {
        setChecked(true);
      }
    }, 0);
    return () => clearTimeout(timer);
  }, [pathname]);

  async function handleLogout() {
    try {
      await logout();
    } finally {
      setUser(null);
      router.push("/login");
    }
  }

  if (!checked) return null;
  if (!user) {
    return (
      <Link className="user-menu" href="/login">
        Entrar
      </Link>
    );
  }
  return (
    <span className="user-menu">
      {user.email}{" "}
      <button type="button" className="link-button" onClick={handleLogout}>
        Sair
      </button>
    </span>
  );
}
