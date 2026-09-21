import React, { useEffect, useState } from "react";
import { Pressable, ScrollView, Text, TextInput, View } from "react-native";
import Ionicons from "@expo/vector-icons/Ionicons";
import { useApp } from "../hooks/AppContext";
import { useResource } from "../hooks/useResource";
import { catalogueLetter } from "../lib/logic";
import { query } from "../lib/api";
import type { AppConfig, Category, List, Song } from "../lib/types";
import {
  Button,
  Copy,
  Empty,
  Loading,
  Notice,
  Page,
  Pill,
  useTheme,
} from "../components/ui";
import { SongRow } from "../components/SongRow";

const alphabets = {
  latin: "ABCDEFGHIJKLMNOPQRSTUVWXYZ".split(""),
  malayalam: [
    "അ",
    "ആ",
    "ഇ",
    "ഈ",
    "ഉ",
    "ഊ",
    "എ",
    "ഏ",
    "ഐ",
    "ഒ",
    "ഓ",
    "ഔ",
    "ക",
    "ഖ",
    "ഗ",
    "ഘ",
    "ങ",
    "ച",
    "ഛ",
    "ജ",
    "ഝ",
    "ഞ",
    "ട",
    "ഠ",
    "ഡ",
    "ഢ",
    "ണ",
    "ത",
    "ഥ",
    "ദ",
    "ധ",
    "ന",
    "പ",
    "ഫ",
    "ബ",
    "ഭ",
    "മ",
    "യ",
    "ര",
    "ല",
    "വ",
    "ശ",
    "ഷ",
    "സ",
    "ഹ",
    "ള",
    "ഴ",
    "റ",
  ],
};

export function CatalogScreen({
  openSong,
}: {
  openSong: (id: string) => void;
}) {
  const theme = useTheme();
  const { revision, favorites, track } = useApp();
  const [search, setSearch] = useState("");
  const [debounced, setDebounced] = useState("");
  const [mode, setMode] = useState<"all" | "recent" | "featured">("all");
  const [alphabet, setAlphabet] = useState<"latin" | "malayalam">("latin");
  const [category, setCategory] = useState("");
  const [categorySearch, setCategorySearch] = useState("");
  const [letter, setLetter] = useState("");
  const [picker, setPicker] = useState<"categories" | "alphabet" | null>(null);
  const [offset, setOffset] = useState(0);
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebounced(search.trim());
      setOffset(0);
    }, 350);
    return () => clearTimeout(timer);
  }, [search]);
  const catalogue = useResource<List<Song>>(
    `/songs?${query({ q: debounced, category, letter: catalogueLetter(debounced, letter), featured: mode === "featured" ? true : undefined, sort: mode === "recent" ? "recent" : "title", limit: 30, offset })}`,
    revision,
  );
  const categories = useResource<List<Category>>("/categories", revision);
  const config = useResource<AppConfig>("/config", revision);
  useEffect(() => {
    if (debounced && catalogue.data && !catalogue.loading)
      track({
        event: "search",
        resultCount: catalogue.data.total ?? catalogue.data.items.length,
      });
  }, [debounced, catalogue.data, catalogue.loading]);
  const selectCategory = (id: string) => {
    setCategory(id);
    setOffset(0);
    setPicker(null);
    setCategorySearch("");
    if (id) track({ event: "category_open", categoryId: id });
  };
  const selectedCategory = categories.data?.items.find(
    (item) => item.id === category,
  );
  const matchingCategories =
    categories.data?.items.filter((item) =>
      `${item.name} ${item.nameMalayalam}`
        .toLocaleLowerCase()
        .includes(categorySearch.trim().toLocaleLowerCase()),
    ) ?? [];
  const updateSearch = (value: string) => {
    setSearch(value);
    if (value.trim()) {
      setLetter("");
      if (picker === "alphabet") setPicker(null);
    }
  };
  return (
    <Page>
      <View style={{ gap: 8 }}>
        <View
          style={{
            flexDirection: "row",
            alignItems: "center",
            borderWidth: 1,
            borderColor: theme.controlBorder,
            borderRadius: 10,
            backgroundColor: theme.surface,
            paddingLeft: 15,
            minHeight: 48,
          }}
        >
          <Ionicons name="search-outline" size={21} color={theme.muted} />
          <TextInput
            accessibilityLabel="Search hymns by title or lyrics"
            placeholder="Search hymns"
            placeholderTextColor={theme.muted}
            value={search}
            onChangeText={updateSearch}
            autoCorrect={false}
            returnKeyType="search"
            style={{
              flex: 1,
              minHeight: 46,
              paddingVertical: 10,
              paddingHorizontal: 12,
              color: theme.ink,
              fontSize: 14,
            }}
          />
          {!!search && (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel="Clear search"
              onPress={() => updateSearch("")}
              style={{
                minWidth: 48,
                minHeight: 48,
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              <Ionicons name="close-circle" size={21} color={theme.muted} />
            </Pressable>
          )}
        </View>
        <View
          style={{
            flexDirection: "row",
            borderBottomWidth: 1,
            borderColor: theme.line,
          }}
        >
          {(["all", "recent", "featured"] as const).map((value) => (
            <Pressable
              key={value}
              accessibilityRole="tab"
              accessibilityState={{ selected: mode === value }}
              onPress={() => {
                setMode(value);
                setOffset(0);
              }}
              style={({ pressed }) => ({
                flex: 1,
                minHeight: 48,
                paddingVertical: 12,
                paddingHorizontal: 4,
                alignItems: "center",
                justifyContent: "center",
                borderBottomWidth: 2,
                borderColor: mode === value ? theme.accent : "transparent",
                opacity: pressed ? 0.6 : 1,
              })}
            >
              <Text
                style={{
                  width: "100%",
                  textAlign: "center",
                  fontSize: 13,
                  fontWeight: mode === value ? "600" : "400",
                  color: mode === value ? theme.ink : theme.muted,
                }}
              >
                {{ all: "All", recent: "Recent", featured: "Featured" }[value]}
              </Text>
            </Pressable>
          ))}
        </View>
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
          <FilterButton
            title="Categories"
            expanded={picker === "categories"}
            active={!!category}
            onPress={() =>
              setPicker(picker === "categories" ? null : "categories")
            }
          />
          {!search.trim() && (
            <FilterButton
              title="A–Z / അ–ഹ"
              expanded={picker === "alphabet"}
              active={!!letter}
              onPress={() =>
                setPicker(picker === "alphabet" ? null : "alphabet")
              }
            />
          )}
        </View>
        {(!!category || !!letter) && (
          <View
            style={{
              flexDirection: "row",
              flexWrap: "wrap",
              alignItems: "center",
              gap: 8,
            }}
          >
            <Text
              style={{
                flexGrow: 1,
                flexBasis: 150,
                color: theme.muted,
                fontSize: 13,
                lineHeight: 22,
                fontFamily:
                  selectedCategory?.nameMalayalam ||
                  (letter && alphabet === "malayalam")
                    ? "Malayalam"
                    : undefined,
              }}
            >
              {[
                selectedCategory?.nameMalayalam ||
                  selectedCategory?.name ||
                  (category ? "Selected category" : ""),
                letter ? `Starts with ${letter}` : "",
              ]
                .filter(Boolean)
                .join(" · ")}
            </Text>
            <Button
              title="Clear filters"
              variant="quiet"
              small
              onPress={() => {
                setCategory("");
                setLetter("");
                setOffset(0);
              }}
            />
          </View>
        )}
      </View>
      {picker === "categories" && (
        <View
          style={{
            gap: 8,
            paddingBottom: 16,
            borderBottomWidth: 1,
            borderColor: theme.line,
          }}
        >
          <TextInput
            accessibilityLabel="Find a category"
            placeholder="Find a category"
            placeholderTextColor={theme.muted}
            value={categorySearch}
            onChangeText={setCategorySearch}
            style={{
              minHeight: 48,
              padding: 12,
              fontSize: 13,
              color: theme.ink,
              borderWidth: 1,
              borderColor: theme.controlBorder,
              borderRadius: 8,
            }}
          />
          {categories.loading ? (
            <Loading />
          ) : categories.error ? (
            <Empty
              title="Categories unavailable"
              detail={categories.error}
              retry={categories.reload}
            />
          ) : (
            <ScrollView
              nestedScrollEnabled
              keyboardShouldPersistTaps="handled"
              style={{ maxHeight: 320 }}
            >
              <CategoryOption
                label="All categories"
                active={!category}
                onPress={() => selectCategory("")}
              />
              {(["purpose", "occasion", "theme"] as const).map((kind) => {
                const items = matchingCategories.filter(
                  (item) => item.kind === kind,
                );
                return items.length ? (
                  <View key={kind}>
                    <Copy
                      muted
                      style={{ marginTop: 16, marginBottom: 6, fontSize: 13 }}
                    >
                      {
                        {
                          purpose: "Purpose",
                          occasion: "Occasion",
                          theme: "Theme",
                        }[kind]
                      }
                    </Copy>
                    {items.map((item) => (
                      <CategoryOption
                        key={item.id}
                        label={item.nameMalayalam || item.name}
                        active={category === item.id}
                        onPress={() => selectCategory(item.id)}
                      />
                    ))}
                  </View>
                ) : null;
              })}
              {!matchingCategories.length && (
                <Copy muted style={{ paddingVertical: 16 }}>
                  No matching categories.
                </Copy>
              )}
            </ScrollView>
          )}
        </View>
      )}
      {picker === "alphabet" && (
        <View
          style={{
            gap: 10,
            paddingBottom: 16,
            borderBottomWidth: 1,
            borderColor: theme.line,
          }}
        >
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
            <Pill
              label="A–Z"
              active={alphabet === "latin"}
              onPress={() => {
                setAlphabet("latin");
                setLetter("");
                setOffset(0);
              }}
            />
            <Pill
              label="അ–ഹ"
              active={alphabet === "malayalam"}
              onPress={() => {
                setAlphabet("malayalam");
                setLetter("");
                setOffset(0);
              }}
            />
          </View>
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
            {alphabets[alphabet].map((value) => (
              <Pressable
                key={value}
                accessibilityRole="button"
                accessibilityLabel={`Hymns starting with ${value}`}
                accessibilityState={{ selected: letter === value }}
                onPress={() => {
                  setLetter(value);
                  setOffset(0);
                  setPicker(null);
                }}
                style={({ pressed }) => ({
                  minWidth: 48,
                  minHeight: 48,
                  padding: 12,
                  borderRadius: 8,
                  backgroundColor:
                    letter === value ? theme.tint : theme.surface,
                  borderWidth: 1,
                  borderColor: letter === value ? theme.accent : theme.line,
                  alignItems: "center",
                  justifyContent: "center",
                  opacity: pressed ? 0.6 : 1,
                })}
              >
                <Text
                  style={{
                    color: theme.ink,
                    fontSize: 14,
                    fontFamily:
                      alphabet === "malayalam" ? "Malayalam" : undefined,
                  }}
                >
                  {value}
                </Text>
              </Pressable>
            ))}
          </View>
        </View>
      )}
      {!!config.data?.announcement && (
        <Notice text={config.data.announcement} />
      )}
      <View>
        {catalogue.data && (
          <Text
            accessibilityLiveRegion="polite"
            style={{ fontSize: 13, color: theme.muted, marginBottom: 4 }}
          >
            {catalogue.data.total ?? catalogue.data.items.length} hymns
            {debounced ? " found" : ""}
          </Text>
        )}
        {catalogue.loading && !catalogue.data ? (
          <Loading />
        ) : catalogue.error ? (
          <Empty
            title="Unable to load hymns"
            detail={catalogue.error}
            retry={catalogue.reload}
          />
        ) : !catalogue.data?.items.length ? (
          <Empty
            title={
              debounced || category || letter || mode !== "all"
                ? "No matching hymns"
                : "No hymns yet"
            }
            detail={
              debounced || category || letter || mode !== "all"
                ? "Try another spelling or clear your filters."
                : "Published hymns will appear here."
            }
          />
        ) : (
          catalogue.data.items.map((song) => (
            <SongRow
              key={song.id}
              song={song}
              saved={favorites.includes(song.id)}
              onPress={() => openSong(song.id)}
            />
          ))
        )}
        {catalogue.data &&
          (offset > 0 || catalogue.data.items.length === 30) && (
            <View
              style={{
                flexDirection: "row",
                justifyContent: "space-between",
                marginTop: 20,
              }}
            >
              <Button
                title="Previous"
                variant="outline"
                disabled={offset === 0 || catalogue.loading}
                onPress={() => setOffset(Math.max(0, offset - 30))}
              />
              <Button
                title="Next"
                variant="outline"
                disabled={catalogue.data.items.length < 30 || catalogue.loading}
                onPress={() => setOffset(offset + 30)}
              />
            </View>
          )}
      </View>
    </Page>
  );
}

function FilterButton({
  title,
  expanded,
  active,
  onPress,
}: {
  title: string;
  expanded: boolean;
  active: boolean;
  onPress: () => void;
}) {
  const theme = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ expanded }}
      onPress={onPress}
      style={({ pressed }) => ({
        flexGrow: 1,
        flexBasis: 130,
        minHeight: 48,
        paddingHorizontal: 12,
        paddingVertical: 10,
        borderRadius: 8,
        borderWidth: 1,
        borderColor: active ? theme.accent : theme.line,
        backgroundColor: active ? theme.tint : "transparent",
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 8,
        opacity: pressed ? 0.6 : 1,
      })}
    >
      <Text
        style={{
          flexShrink: 1,
          fontSize: 13,
          lineHeight: 22,
          color: theme.ink,
          fontFamily: /[\u0D00-\u0D7F]/.test(title) ? "Malayalam" : undefined,
        }}
      >
        {title}
      </Text>
      <Ionicons
        name={expanded ? "chevron-up" : "chevron-down"}
        size={17}
        color={theme.muted}
      />
    </Pressable>
  );
}
function CategoryOption({
  label,
  active,
  onPress,
}: {
  label: string;
  active: boolean;
  onPress: () => void;
}) {
  const theme = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ selected: active }}
      onPress={onPress}
      style={({ pressed }) => ({
        minHeight: 48,
        paddingVertical: 12,
        paddingHorizontal: 10,
        borderRadius: 6,
        backgroundColor: active ? theme.tint : "transparent",
        flexDirection: "row",
        alignItems: "center",
        gap: 10,
        opacity: pressed ? 0.6 : 1,
      })}
    >
      <Text
        style={{
          flex: 1,
          color: theme.ink,
          fontSize: 13,
          lineHeight: 22,
          fontFamily: /[\u0D00-\u0D7F]/.test(label) ? "Malayalam" : undefined,
        }}
      >
        {label}
      </Text>
      {active && <Ionicons name="checkmark" size={20} color={theme.accent} />}
    </Pressable>
  );
}
