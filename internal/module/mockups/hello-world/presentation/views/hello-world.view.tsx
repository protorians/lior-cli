"use client"

import {Footer} from "@/core/presentation/themes/katon/footer";
import {View} from "@sentients/sdk/presentation/themes/katon/view";
import {Header} from "@sentients/sdk/presentation/themes/katon/header";
import {Main} from "@sentients/sdk/presentation/themes/katon/main";
import {Wrapper} from "@/core/presentation/themes/katon/wrapper";
import {AnimatedContent} from "@sentients/sdk/presentation/components/animated-content";
import {HelloWorldDataGrid} from "../components/hello-world-data-grid";
import {CreateHelloWorldDialog} from "../components/create-hello-world-dialog";

export function HelloWorldView() {
    return (
        <View>
            <Wrapper>
                <Header/>
                <Main className="flex flex-col lg:flex-row p-6 gap-6">
                    <AnimatedContent variant="container" animateChildren className="contents">
                        <div className="flex-auto flex flex-col gap-6">
                            <div className="flex flex-row items-center">
                                <div className="flex flex-col flex-auto overflow-hidden">
                                    <h1 className="text-2xl font-bold truncate text-ellipsis">
                                        Hello World
                                    </h1>
                                    <p className="text-muted-foreground">
                                        Module d&apos;exemple pour l&apos;onboarding
                                    </p>
                                </div>
                                <div className="flex flex-row items-center gap-2">
                                    <CreateHelloWorldDialog/>
                                </div>
                            </div>

                            <div className="flex flex-col gap-4 min-h-[40dvh]">
                                <HelloWorldDataGrid/>
                            </div>
                        </div>
                    </AnimatedContent>
                </Main>
            </Wrapper>
            <Footer/>
        </View>
    );
}