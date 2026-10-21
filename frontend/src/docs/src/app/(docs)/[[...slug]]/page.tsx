// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { notFound } from "next/navigation";
import type { Metadata } from "next";
import { createRelativeLink } from "fumadocs-ui/mdx";
import { DocsBody, DocsDescription, DocsPage, DocsTitle } from "fumadocs-ui/layouts/docs/page";
import { getMDXComponents } from "@/components/mdx";
import { source } from "@/lib/source";
import { COMPANY } from "@/lib/constants";
import { visibleProductName } from "@/lib/branding";

export default async function Page(props: PageProps<"/[[...slug]]">) {
  const params = await props.params;
  const page = source.getPage(params.slug);
  if (!page) notFound();

  const MDX = page.data.body;

  return (
    <DocsPage toc={page.data.toc} full={page.data.full}>
      <DocsTitle>{visibleProductName(page.data.title)}</DocsTitle>
      <DocsDescription>{page.data.description && visibleProductName(page.data.description)}</DocsDescription>
      <DocsBody>
        <MDX
          components={getMDXComponents({
            a: createRelativeLink(source, page),
          })}
        />
      </DocsBody>
    </DocsPage>
  );
}

export async function generateStaticParams() {
  return source.generateParams();
}

export async function generateMetadata(props: PageProps<"/[[...slug]]">): Promise<Metadata> {
  const params = await props.params;
  const page = source.getPage(params.slug);
  if (!page) notFound();

  return {
    title: `${visibleProductName(page.data.title)} | ${COMPANY.NAME} Docs`,
    description: page.data.description && visibleProductName(page.data.description),
  };
}
