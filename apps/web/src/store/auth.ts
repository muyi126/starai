import { create } from "zustand";
import type { User } from "@starai/shared-types";
import { API_URL, clearUserSession } from "@/lib/api";

interface AuthState {
  token: string | null;
  user: User | null;
  setAuth: (token: string, user: User) => void;
  logout: () => void;
  hydrate: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  token: null,
  user: null,
  setAuth: (token, user) => {
    try {
      localStorage.removeItem("token");
      localStorage.setItem("starai_session", "1");
      localStorage.setItem("user", JSON.stringify(user));
    } catch { /* the session cookie and in-memory user remain usable */ }
    set({ token: "session", user });
  },
  logout: () => {
    void fetch(`${API_URL}/api/auth/logout`, { method: "POST", credentials: "include" }).catch(() => {});
    clearUserSession();
    set({ token: null, user: null });
  },
  hydrate: () => {
    let token, session, userStr;
    try {
      token = localStorage.getItem("token");
      session = localStorage.getItem("starai_session") === "1";
      userStr = localStorage.getItem("user");
    } catch { return; }
    if ((session || token) && userStr) {
      try {
        set({ token: session ? "session" : token, user: JSON.parse(userStr) });
      } catch {
        clearUserSession();
      }
    }
  },
}));
