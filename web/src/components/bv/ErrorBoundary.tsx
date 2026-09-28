// ErrorBoundary (PRD P0 §23 Error State): error render yang tidak tertangani → halaman pemulihan
// (apa yang terjadi · dampak · aksi pemulihan), bukan layar putih. Dipakai di level app dan per route (resetKey = path).
import * as React from "react";
import { ErrorRecoveryPage } from "@/components/shell/SystemPages";
import { reportClientError } from "@/lib/client-errors";

interface Props {
  children: React.ReactNode;
  /** Berubah → boundary di-reset (mis. pathname saat navigasi). */
  resetKey?: unknown;
  fallback?: (error: Error, reset: () => void) => React.ReactNode;
  /** Tampilan dalam shell (tanpa layar penuh). */
  inline?: boolean;
}
interface State { error: Error | null; key: unknown }

export class ErrorBoundary extends React.Component<Props, State> {
  state: State = { error: null, key: this.props.resetKey };

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error };
  }

  static getDerivedStateFromProps(props: Props, state: State): Partial<State> | null {
    if (props.resetKey !== state.key) return { key: props.resetKey, error: null };
    return null;
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    // Observability (PRD P0 v2 §24.4): console + laporan best-effort ke server (rate-limited, hanya saat login).
    console.error("[BuildingVision] render error", error, info.componentStack);
    reportClientError(error, { componentStack: info.componentStack });
  }

  reset = () => this.setState({ error: null });

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    if (this.props.fallback) return this.props.fallback(error, this.reset);
    return <ErrorRecoveryPage error={error} onReset={this.reset} inline={this.props.inline} />;
  }
}
