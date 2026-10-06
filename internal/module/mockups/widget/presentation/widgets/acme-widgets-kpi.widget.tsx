"use client"
import * as React from "react"
import {LayoutDashboardIcon} from "lucide-react"
import {ModuleWidget} from "@liorian/sdk/presentation/module-widget"
import {useQuery} from "@tanstack/react-query"
import {useAuth} from "@liorian/sdk/infrastructure/hooks/use-auth"
import {AcmeWidgetsApiService} from "../../application/service/acme-widgets-api-service";
import {AcmeWidgetsAnalyticsInterface} from "../../domain/acme-widgets.interface";

/**
 * Widget KPI du tableau de bord, rendu par `ModuleWidget` (SDK).
 *
 * La donnée passe par le service d'API du module (`ModuleApiService`, proxy
 * `ctx.api`) — jamais par un appel réseau direct.
 */
export function AcmeWidgetsKpiWidget() {
    const {currentOrganization} = useAuth()

    const {data: analytics, isLoading} = useQuery<AcmeWidgetsAnalyticsInterface>({
        queryKey: ['acme-widgets', 'kpi'],
        enabled: !!currentOrganization?.id,
        queryFn: async () => {
            const response = await AcmeWidgetsApiService.getAnalytics()
            const payload = response.data as any;
            return payload?.data ?? payload;
        },
    })

    return (
        <ModuleWidget
            title={
                <div className="flex items-center gap-2">
                    <LayoutDashboardIcon className="size-5 text-primary"/>
                    <span>Acme Widgets</span>
                </div>
            }
            description="Indicateurs du module Acme Widgets"
            stats={[
                {label: 'Total', amount: analytics?.total ?? 0},
                {label: 'En cours', amount: analytics?.inProgress ?? 0},
                {label: 'Terminés', amount: analytics?.completed ?? 0},
                {label: 'Archivés', amount: analytics?.archived ?? 0},
            ]}
            loading={isLoading}
            className="h-full"
        />
    )
}
