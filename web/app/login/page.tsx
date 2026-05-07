import Link from "next/link";
import LoginForm from "./login-form";
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
 * Login page — only renders active sign-in when web auth is available.
 */
export default function LoginPage() {
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
            <CardTitle>Sign in is unavailable in local mode</CardTitle>
            <CardDescription>
              Local mode is single-user and does not require authentication.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              Open the app directly and continue without signing in.
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

  return <LoginForm />;
}
