"use client"

import {View} from "@liorian/sdk/presentation/themes/katon/view";
import {AutoBreadcrumb} from "@liorian/sdk/presentation/components/auto-breadcrumb";
import {Activity} from "@liorian/sdk/presentation/components/activity";
import {SystemConsoleDiagnostics} from "../components/system-console-diagnostics";

/**
 * Vue principale du module SYSTEM. Rendue uniquement sous Tauri : le socle
 * dérive `platform_unsupported` du couple type × runtime (ADR-004) et ne
 * monte pas le module sur web.
 */
export function SystemConsoleView() {
    return (
        <View>
            <View.Wrapper>
                <View.Helmet/>
                <View.Frame className="flex flex-col lg:flex-row p-6 gap-6">
                    <Activity.Container variant="container" animateChildren className="contents">
                        <Activity.Content>
                            <Activity.Header>
                                <Activity.Title
                                    label="Console système"
                                    description="Diagnostics et maintenance — accès administrateur, Tauri uniquement"
                                />
                            </Activity.Header>

                            <div className="flex flex-col gap-4 min-h-[40dvh]">
                                <SystemConsoleDiagnostics/>
                            </div>
                        </Activity.Content>
                    </Activity.Container>
                </View.Frame>
            </View.Wrapper>
            <View.Status breadcrumb={<AutoBreadcrumb/>}/>
        </View>
    );
}
