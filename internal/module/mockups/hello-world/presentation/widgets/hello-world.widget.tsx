"use client"
import * as React from "react"
import {WandSparklesIcon} from "lucide-react"
import {ModuleWidget} from "@liorian/sdk/presentation/module-widget"
import {useHelloWorldAnalytics} from "../../application/hooks/use-hello-world-analytics";

export function HelloWorldWidget() {
    const {data: analytics, isLoading} = useHelloWorldAnalytics();

    return (
        <ModuleWidget
            title={
                <div className="flex items-center gap-2">
                    <WandSparklesIcon className="size-5 text-primary"/>
                    <span>Hello World</span>
                </div>
            }
            description="Module d'exemple pour l'onboarding"
            stats={[
                {label: 'Total', amount: analytics?.total ?? 0},
                {label: 'Publiés', amount: analytics?.published ?? 0},
                {label: 'Brouillons', amount: analytics?.drafts ?? 0},
                {label: 'Archivés', amount: analytics?.archived ?? 0},
            ]}
            loading={isLoading}
            className="h-full"
        />
    )
}
