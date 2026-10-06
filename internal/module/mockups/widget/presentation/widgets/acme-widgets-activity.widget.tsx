"use client"
import * as React from "react"
import {ActivityIcon} from "lucide-react"
import {Empty, EmptyDescription, EmptyMedia, EmptyTitle} from "@liorian/sdk/presentation/ui/empty"
import {Skeleton} from "@liorian/sdk/presentation/ui/skeleton"
import {useQuery} from "@tanstack/react-query"
import {useAuth} from "@liorian/sdk/infrastructure/hooks/use-auth"
import {AcmeWidgetsApiService} from "../../application/service/acme-widgets-api-service";
import {AcmeWidgetsActivityInterface} from "../../domain/acme-widgets.interface";

/**
 * Widget d'activité : second style de widget, une liste simple rendue hors
 * `ModuleWidget` pour montrer qu'un widget est un composant React libre tant
 * qu'il reste autonome (données par `ctx.api`, aucune dépendance au socle).
 */
export function AcmeWidgetsActivityWidget() {
    const {currentOrganization} = useAuth()

    const {data: activities, isLoading} = useQuery<AcmeWidgetsActivityInterface[]>({
        queryKey: ['acme-widgets', 'activity'],
        enabled: !!currentOrganization?.id,
        queryFn: async () => {
            const response = await AcmeWidgetsApiService.getRecentActivities({limit: 5})
            const payload = response.data as any;
            return (Array.isArray(payload) ? payload : payload?.data) ?? [];
        },
    })

    if (isLoading) {
        return (
            <div className="flex flex-col gap-2 p-4 h-full">
                <Skeleton className="h-4 w-1/3"/>
                <Skeleton className="h-4 w-2/3"/>
                <Skeleton className="h-4 w-1/2"/>
            </div>
        )
    }

    if (!activities?.length) {
        return (
            <div className="flex flex-col items-center justify-center h-full p-4">
                <Empty>
                    <EmptyMedia>
                        <ActivityIcon size={40} strokeWidth={1}/>
                    </EmptyMedia>
                    <EmptyTitle>Aucune activité</EmptyTitle>
                    <EmptyDescription>Les dernières activités s&apos;afficheront ici</EmptyDescription>
                </Empty>
            </div>
        )
    }

    return (
        <div className="flex flex-col gap-3 p-4 h-full overflow-y-auto">
            <div className="flex items-center gap-2 text-sm font-semibold">
                <ActivityIcon className="size-4 text-primary"/>
                Activité récente
            </div>
            <ul className="flex flex-col gap-2 text-sm">
                {activities.map((activity) => (
                    <li key={activity.id} className="flex items-center justify-between gap-2 border-b border-border/50 pb-2 last:border-0 last:pb-0">
                        <span className="truncate">{activity.label}</span>
                        <span className="text-xs text-muted-foreground shrink-0">
                            {activity.at ? new Date(activity.at).toLocaleDateString('fr-FR') : "—"}
                        </span>
                    </li>
                ))}
            </ul>
        </div>
    )
}
