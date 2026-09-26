"use client"

import {View} from "@liorian/sdk/presentation/themes/katon/view";
import {AutoBreadcrumb} from "@/core/presentation/components/auto-breadcrumb";
import {Activity} from "@liorian/sdk/presentation/components/activity";

export function HelloWorldView() {
    return (
        <View>
            <View.Wrapper>
                <View.Helmet/>
                <View.Frame className="flex flex-col lg:flex-row p-6 gap-6">
                    <Activity.Container variant="container" animateChildren className="contents">
                        <Activity.Content>
                            <Activity.Header>
                                <Activity.Title
                                    label="Hello World"
                                    description="Module d'exemple pour l'onboarding"
                                />
                            </Activity.Header>

                            <div className="flex flex-col gap-4 min-h-[40dvh]">
                                <h1>Welcome to Liora Socle</h1>
                            </div>
                        </Activity.Content>
                    </Activity.Container>
                </View.Frame>
            </View.Wrapper>
            <View.Status breadcrumb={<AutoBreadcrumb/>}/>
        </View>
    );
}
