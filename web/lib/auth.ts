import NextAuth from "next-auth";
import Credentials from "next-auth/providers/credentials";
import { getPool } from "@/lib/db-pool";
import { verifyPassword } from "@/lib/password";
import { canonicalizeEmailIdentity } from "@/lib/email-identity";

export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [
    Credentials({
      credentials: {
        email: { label: "Email", type: "email" },
        password: { label: "Password", type: "password" },
      },
      async authorize(credentials) {
        if (
          typeof credentials?.email !== "string" ||
          typeof credentials?.password !== "string"
        ) {
          return null;
        }

        const canonicalEmail = canonicalizeEmailIdentity(credentials.email);
        if (!canonicalEmail.includes("@")) {
          return null;
        }

        const pool = getPool();
        const { rows } = await pool.query(
          "SELECT id, email, name, password_hash FROM users WHERE email = $1",
          [canonicalEmail],
        );

        if (rows.length === 0) {
          return null;
        }

        const user = rows[0];
        const valid = await verifyPassword(
          credentials.password,
          user.password_hash,
        );
        if (!valid) {
          return null;
        }

        return {
          id: user.id,
          email: user.email,
          name: user.name ?? null,
        };
      },
    }),
  ],
  session: { strategy: "jwt", maxAge: 90 * 24 * 60 * 60 }, // 90 days
  pages: {
    signIn: "/login",
  },
  callbacks: {
    jwt({ token, user }) {
      if (user) {
        token.userId = user.id!;
      }
      return token;
    },
    session({ session, token }) {
      session.user.id = token.userId;
      return session;
    },
  },
});
