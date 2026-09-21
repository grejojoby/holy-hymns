import React, { useCallback, useEffect, useState } from "react";
import { BackHandler, Modal, View } from "react-native";
import { useApp } from "../hooks/AppContext";
import { useResource } from "../hooks/useResource";
import { api, message, post, put } from "../lib/api";
import { parseLinks } from "../lib/logic";
import type {
  Category,
  List,
  Song,
  SongInput,
  SongRevision,
} from "../lib/types";
import {
  Button,
  Copy,
  Empty,
  Field,
  Heading,
  Loading,
  Notice,
  Page,
  Panel,
  Pill,
  Toggle,
  useTheme,
} from "../components/ui";
import { SongContent } from "./SongScreen";

const empty: SongInput = {
  title: "",
  titleMalayalam: "",
  lyricsMalayalam: "",
  lyricsManglish: "",
  aliases: [],
  credits: "",
  links: [],
  categoryIds: [],
  featured: false,
};
export function SongEditor({
  id,
  close,
}: {
  id: string | null;
  close: () => void;
}) {
  const theme = useTheme();
  const { invalidate, revision } = useApp();
  const [song, setSong] = useState<Song | null>(null);
  const [form, setForm] = useState<SongInput>(empty);
  const [aliases, setAliases] = useState("");
  const [links, setLinks] = useState("");
  const [loading, setLoading] = useState(!!id);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [preview, setPreview] = useState(false);
  const [history, setHistory] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [discard, setDiscard] = useState(false);
  const goBack = useCallback(() => {
    if (dirty) setDiscard(true);
    else close();
  }, [close, dirty]);
  useEffect(() => {
    const subscription = BackHandler.addEventListener(
      "hardwareBackPress",
      () => {
        goBack();
        return true;
      },
    );
    return () => subscription.remove();
  }, [goBack]);
  const categories = useResource<List<Category>>("/categories", revision);
  const revisions = useResource<List<SongRevision>>(
    history && song ? `/admin/songs/${song.id}/revisions` : null,
    song?.version,
  );
  const populate = (value: Song) => {
    setSong(value);
    setForm({
      title: value.title,
      titleMalayalam: value.titleMalayalam,
      lyricsMalayalam: value.lyricsMalayalam,
      lyricsManglish: value.lyricsManglish,
      aliases: value.aliases || [],
      links: value.links || [],
      categoryIds: value.categoryIds || [],
      credits: value.credits,
      featured: value.featured,
    });
    setAliases((value.aliases || []).join(", "));
    setLinks(
      (value.links || [])
        .map((link) => `${link.label} | ${link.url}`)
        .join("\n"),
    );
    setDirty(false);
  };
  const load = async () => {
    if (!id && !song) return;
    setLoading(true);
    setError("");
    try {
      populate(await api<Song>(`/admin/songs/${song?.id || id}`));
    } catch (error) {
      setError(message(error));
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    if (id) void load();
  }, [id]);
  const change = <K extends keyof SongInput>(key: K, value: SongInput[K]) => {
    setForm((previous) => ({ ...previous, [key]: value }));
    setDirty(true);
  };
  const input = (): SongInput => ({
    ...form,
    aliases: aliases
      .split(",")
      .map((value) => value.trim())
      .filter(Boolean),
    links: parseLinks(links),
  });
  const save = async () => {
    const body = input();
    const result = song
      ? await put<Song>(`/admin/songs/${song.id}`, {
          ...body,
          version: song.version,
        })
      : await post<Song>("/admin/songs", body);
    populate(result);
    return result;
  };
  const run = async (task: () => Promise<void>) => {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await task();
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  };
  const publish = () =>
    run(async () => {
      const saved = dirty || !song ? await save() : song;
      await post(`/admin/songs/${saved.id}/publish`, {
        version: saved.version,
      });
      populate(await api<Song>(`/admin/songs/${saved.id}`));
      invalidate();
      setNotice("Hymn published.");
    });
  return (
    <Page>
      <View
        style={{
          flexDirection: "row",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <Button
          title="Songs"
          icon="arrow-back"
          variant="quiet"
          onPress={goBack}
        />
        <Pill
          label={preview ? "Edit" : "Preview"}
          active={preview}
          onPress={() => setPreview((value) => !value)}
        />
      </View>
      <Modal
        visible={discard}
        transparent
        animationType="fade"
        onRequestClose={() => setDiscard(false)}
      >
        <View
          style={{
            flex: 1,
            justifyContent: "center",
            padding: 24,
            backgroundColor: "rgba(0, 0, 0, 0.4)",
          }}
        >
          <View
            style={{ width: "100%", maxWidth: 480, alignSelf: "center" }}
            accessibilityViewIsModal
          >
            <Panel>
              <Copy>Leave without saving your latest changes?</Copy>
              <Button title="Keep editing" onPress={() => setDiscard(false)} />
              <Button
                title="Discard changes"
                variant="danger"
                onPress={close}
              />
            </Panel>
          </View>
        </View>
      </Modal>
      <View style={{ gap: 8 }}>
        <Heading level={2}>{song ? "Edit hymn" : "New hymn"}</Heading>
        {song && (
          <Copy muted style={{ fontSize: 14 }}>
            {song.status === "published" ? "Published" : "Draft"} · Version{" "}
            {song.version}
          </Copy>
        )}
        <Copy muted>Draft changes are private until you publish.</Copy>
      </View>
      <Notice text={error} error />
      <Notice text={notice} />
      {!!error && song && (
        <Button
          title="Discard edits & load latest version"
          variant="outline"
          onPress={() => {
            void load();
          }}
        />
      )}
      {loading ? (
        <Loading />
      ) : !song && id ? (
        <Empty
          title="Unable to open editor"
          detail={error}
          retry={() => {
            void load();
          }}
        />
      ) : preview ? (
        <SongContent
          song={{
            ...form,
            id: song?.id || "",
            aliases: aliases.split(","),
            links: (() => {
              try {
                return parseLinks(links);
              } catch {
                return [];
              }
            })(),
            status: "draft",
            version: song?.version || 0,
            updatedAt: song?.updatedAt || "",
          }}
        />
      ) : (
        <>
          {!!song?.reviewNotes?.length && (
            <Panel>
              <Heading level={3}>Import review</Heading>
              {song.reviewNotes.map((note, index) => (
                <Copy key={index}>{note}</Copy>
              ))}
              {!!song.sourceUrl && <Copy muted>Source: {song.sourceUrl}</Copy>}
            </Panel>
          )}
          <Field
            label="Title in Manglish"
            value={form.title}
            onChangeText={(value) => change("title", value)}
          />
          <Field
            label="Malayalam title"
            value={form.titleMalayalam}
            onChangeText={(value) => change("titleMalayalam", value)}
            style={{ fontFamily: "Malayalam" }}
          />
          <Field
            label="Malayalam lyrics"
            value={form.lyricsMalayalam}
            onChangeText={(value) => change("lyricsMalayalam", value)}
            multiline
            style={{ minHeight: 260, fontFamily: "Malayalam", lineHeight: 30 }}
          />
          <Field
            label="Manglish lyrics"
            value={form.lyricsManglish}
            onChangeText={(value) => change("lyricsManglish", value)}
            multiline
            style={{ minHeight: 260, lineHeight: 26 }}
          />
          <Field
            label="Alternate spellings, separated by commas"
            value={aliases}
            onChangeText={(value) => {
              setAliases(value);
              setDirty(true);
            }}
          />
          <Field
            label="Credits"
            value={form.credits}
            onChangeText={(value) => change("credits", value)}
            multiline
          />
          <Field
            label="Links — one per line"
            placeholder="Listen | https://example.com/song"
            value={links}
            onChangeText={(value) => {
              setLinks(value);
              setDirty(true);
            }}
            multiline
            autoCapitalize="none"
            autoCorrect={false}
          />
          <View style={{ gap: 12 }}>
            <Heading level={3}>Categories</Heading>
            {!!categories.error && <Notice text={categories.error} error />}
            <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
              {categories.data?.items.map((category) => (
                <Pill
                  key={category.id}
                  label={category.name}
                  active={form.categoryIds.includes(category.id)}
                  onPress={() =>
                    change(
                      "categoryIds",
                      form.categoryIds.includes(category.id)
                        ? form.categoryIds.filter(
                            (value) => value !== category.id,
                          )
                        : [...form.categoryIds, category.id],
                    )
                  }
                />
              ))}
            </View>
          </View>
          <Toggle
            title="Feature this hymn"
            value={form.featured}
            onChange={(value) => change("featured", value)}
          />
        </>
      )}
      {!loading && (
        <View style={{ gap: 10 }}>
          <Button
            title="Save draft"
            busy={busy}
            variant="outline"
            onPress={() => {
              void run(async () => {
                await save();
                setNotice("Draft saved.");
              });
            }}
          />
          <Button
            title="Publish hymn"
            icon="checkmark-circle-outline"
            busy={busy}
            onPress={() => {
              void publish();
            }}
          />
          {song?.status === "published" && (
            <Button
              title="Unpublish hymn"
              variant="danger"
              busy={busy}
              disabled={dirty}
              onPress={() => {
                void run(async () => {
                  await post(`/admin/songs/${song.id}/unpublish`, {
                    version: song.version,
                  });
                  populate(await api<Song>(`/admin/songs/${song.id}`));
                  invalidate();
                  setNotice("Hymn unpublished.");
                });
              }}
            />
          )}
          {song && (
            <Button
              title={history ? "Hide revision history" : "Revision history"}
              variant="quiet"
              onPress={() => setHistory((value) => !value)}
            />
          )}
        </View>
      )}
      {history && (
        <View style={{ gap: 15 }}>
          <Heading level={2}>Revision history</Heading>
          <Copy muted>
            Restoring creates a new private draft. Save your current edits
            before restoring; review and publish the restored draft separately.
          </Copy>
          {revisions.loading ? (
            <Loading />
          ) : revisions.error ? (
            <Notice error text={revisions.error} />
          ) : (
            revisions.data?.items.map((item) => (
              <View
                key={item.id}
                style={{
                  gap: 8,
                  paddingVertical: 16,
                  borderBottomWidth: 1,
                  borderColor: theme.line,
                }}
              >
                <Copy style={{ fontWeight: "600" }}>
                  Version {item.version}
                </Copy>
                <Copy muted style={{ fontSize: 14 }}>
                  {new Date(item.createdAt).toLocaleString()}
                </Copy>
                <Copy>{item.content.title}</Copy>
                <Button
                  title={`Restore version ${item.version}`}
                  variant="outline"
                  busy={busy}
                  disabled={dirty}
                  onPress={() => {
                    void run(async () => {
                      populate(
                        await post<Song>(`/admin/songs/${song!.id}/restore`, {
                          revisionId: item.id,
                          version: song!.version,
                        }),
                      );
                      setNotice("Revision restored to a draft.");
                    });
                  }}
                />
              </View>
            ))
          )}
        </View>
      )}
    </Page>
  );
}
