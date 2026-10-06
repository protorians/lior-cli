"use client"

import {useQuery} from "@tanstack/react-query";
import {AlertTriangleIcon, CheckCircleIcon, RefreshCwIcon, ShieldAlertIcon, TerminalIcon} from "lucide-react";
import {Badge} from "@liorian/sdk/presentation/ui/badge";
import {Button} from "@liorian/sdk/presentation/ui/button";
import {Empty, EmptyDescription, EmptyMedia, EmptyTitle} from "@liorian/sdk/presentation/ui/empty";
import {Activity} from "@liorian/sdk/presentation/components/activity";
import {SystemConsoleApiService} from "../../application/service/system-console-api-service";
import {SystemConsoleDiagnosticInterface, SystemConsoleHealthInterface} from "../../domain/system-console.interface";
import {SystemConsoleLevel} from "../../domain/enums/system-console-level.enum";

const LEVEL_BADGES: Record<SystemConsoleLevel, { label: string; className: string }> = {
    [SystemConsoleLevel.INFO]: {label: "Info", className: "text-sky-600 border-sky-500/20 bg-sky-500/10"},
    [SystemConsoleLevel.WARNING]: {label: "Avertissement", className: "text-amber-600 border-amber-500/20 bg-amber-500/10"},
    [SystemConsoleLevel.CRITICAL]: {label: "Critique", className: "text-red-600 border-red-500/20 bg-red-500/10"},
};

export function SystemConsoleDiagnostics() {
    const {data, isLoading, refetch, isRefetching} = useQuery<SystemConsoleHealthInterface>({
        queryKey: ['system-console', 'health'],
        queryFn: async () => {
            const response = await SystemConsoleApiService.getHealth();
            const payload = response.data as any;
            return payload?.data ?? payload;
        },
    });

    if (isLoading) {
        return (
            <div className="flex-auto flex items-center justify-center min-h-[30dvh]">
                <Activity.Loader size={24}/>
            </div>
        );
    }

    const diagnostics = data?.diagnostics ?? [];

    if (!diagnostics.length) {
        return (
            <div className="flex-auto flex flex-col items-center justify-center min-h-[30dvh]">
                <Empty>
                    <EmptyMedia>
                        <TerminalIcon size={80} strokeWidth={1}/>
                    </EmptyMedia>
                    <EmptyTitle>Console système</EmptyTitle>
                    <EmptyDescription>Aucun diagnostic remonté pour le moment</EmptyDescription>
                </Empty>
            </div>
        );
    }

    return (
        <div className="flex flex-col gap-3">
            <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    {data?.status === 'HEALTHY' ? (
                        <CheckCircleIcon className="size-4 text-emerald-600"/>
                    ) : (
                        <AlertTriangleIcon className="size-4 text-amber-600"/>
                    )}
                    <span>État général : {data?.status ?? "—"}</span>
                </div>
                <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isRefetching}>
                    <RefreshCwIcon className="size-4"/>
                    Actualiser
                </Button>
            </div>
            <ul className="flex flex-col gap-2">
                {diagnostics.map((diagnostic) => (
                    <DiagnosticRow key={diagnostic.id} diagnostic={diagnostic}/>
                ))}
            </ul>
        </div>
    );
}

function DiagnosticRow({diagnostic}: { diagnostic: SystemConsoleDiagnosticInterface }) {
    const badge = LEVEL_BADGES[diagnostic.level] ?? LEVEL_BADGES[SystemConsoleLevel.INFO];

    return (
        <li className="flex items-start gap-3 rounded-lg border border-border/50 bg-muted/20 p-3">
            <div className="mt-0.5 text-muted-foreground">
                {diagnostic.level === SystemConsoleLevel.CRITICAL
                    ? <ShieldAlertIcon className="size-4 text-red-600"/>
                    : <TerminalIcon className="size-4"/>}
            </div>
            <div className="flex flex-col gap-1 min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-medium">{diagnostic.label}</span>
                    <Badge variant="outline" className={badge.className}>{badge.label}</Badge>
                </div>
                {diagnostic.detail && (
                    <span className="text-xs text-muted-foreground break-words">{diagnostic.detail}</span>
                )}
            </div>
        </li>
    );
}
