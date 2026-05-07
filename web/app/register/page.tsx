import Link from "next/link";
import RegisterForm from "./register-form";
import { getLocalModeState } from "@/lib/local-mode";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export const dynamic = "force-dynamic";

/**
 * Registration page — only renders an active sign-up form when registration
 * is enabled in server configuration.
 */
export default function RegisterPage() {
  const localMode = getLocalModeState();

  if (localMode.hasConfigError) {
    return (
      <div className="flex min-h-screen items-center justify-center px-4">
        <Card className="w-full max-w-sm">
          <CardHeader>
            <CardTitle>Invalid local backend configuration</CardTitle>
            <CardDescription>
              LOCAL_BACKEND_URL is set but invalid. Fix it before using auth pages.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              Use a loopback URL such as <code>http://127.0.0.1:9876</code>.
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (localMode.enabled) {
    return (
      <div className="flex min-h-screen items-center justify-center px-4">
        <Card className="w-full max-w-sm">
          <CardHeader>
            <CardTitle>Registration is unavailable in local mode</CardTitle>
            <CardDescription>
              Local mode is single-user and does not use web authentication.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              Open the app directly instead of creating an account.
            </p>
          </CardContent>
          <CardFooter>
            <Link href="/" className="text-sm underline hover:text-foreground">
              Go to app
            </Link>
          </CardFooter>
        </Card>
      </div>
    );
  }

  if (process.env.REGISTRATION_ENABLED !== "true") {
    return (
      <div className="flex min-h-screen items-center justify-center px-4">
        <Card className="w-full max-w-sm">
          <CardHeader>
            <CardTitle>Registration is disabled</CardTitle>
            <CardDescription>
              New account sign-up is currently turned off by the operator.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              If you already have an account, sign in instead.
            </p>
          </CardContent>
          <CardFooter>
            <Link href="/login" className="text-sm underline hover:text-foreground">
              Go to sign in
            </Link>
          </CardFooter>
        </Card>
      </div>
    );
  }

  return <RegisterForm />;
}
