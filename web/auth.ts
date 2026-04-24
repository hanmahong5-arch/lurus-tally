import NextAuth from "next-auth"
import Zitadel from "next-auth/providers/zitadel"

declare module "next-auth" {
  interface Session {
    user: {
      id: string
      email?: string | null
      name?: string | null
      image?: string | null
      tenantId: string | null
      profileType: string | null
      isFirstTime: boolean
    }
  }

  interface JWT {
    sub?: string
    tallyTenantId?: string
    profileType?: string
  }
}

export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [
    Zitadel({
      clientId: process.env.ZITADEL_CLIENT_ID!,
      issuer: process.env.ZITADEL_ISSUER ?? "https://auth.lurus.cn",
      // PKCE is enabled by default for Zitadel provider.
    }),
  ],
  pages: {
    signIn: "/login",
  },
  callbacks: {
    async jwt({ token, account, profile }) {
      // On first sign-in, store the Zitadel sub and any custom claims.
      if (account) {
        token.sub = token.sub ?? (profile?.sub as string)
        // tally_tenant_id may be injected by a Zitadel Action in the future.
        const p = profile as Record<string, unknown> | undefined
        if (p?.tally_tenant_id && typeof p.tally_tenant_id === "string") {
          token.tallyTenantId = p.tally_tenant_id
        }
      }
      return token
    },
    async session({ session, token }) {
      // Fetch /api/v1/me from the backend to get tenantId + profileType.
      // The backend JWT validation happens server-side when the access_token is forwarded.
      // For MVP, we rely on the tally_tenant_id claim injected into the JWT token cache.
      const tenantId = typeof token.tallyTenantId === "string" ? token.tallyTenantId : null
      const profileType = typeof token.profileType === "string" ? token.profileType : null

      session.user = {
        ...session.user,
        id: token.sub ?? "",
        tenantId,
        profileType,
        isFirstTime: !tenantId,
      }
      return session
    },
  },
})
