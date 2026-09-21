import React, { useEffect, useState } from "react";
import { View } from "react-native";
import { useApp } from "../hooks/AppContext";
import { useResource } from "../hooks/useResource";
import { api, message, post, put } from "../lib/api";
import type { Analytics, AppConfig, Category, List, User } from "../lib/types";
import {
  Button,
  Copy,
  Field,
  Heading,
  Loading,
  Notice,
  Panel,
  Pill,
  useTheme,
} from "../components/ui";

export function CategoryManager() {
  const theme = useTheme();
  const { revision, invalidate } = useApp();
  const categories = useResource<List<Category>>("/categories", revision);
  const [editing, setEditing] = useState<Category | null>(null);
  const [name, setName] = useState("");
  const [malayalam, setMalayalam] = useState("");
  const [kind, setKind] = useState<Category["kind"]>("theme");
  const [position, setPosition] = useState("0");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [removeID, setRemoveID] = useState("");
  const reset = () => {
    setEditing(null);
    setName("");
    setMalayalam("");
    setKind("theme");
    setPosition("0");
  };
  const run = async (task: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await task();
      invalidate();
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <View style={{ gap: 24 }}>
      <Heading level={2}>Categories</Heading>
      <Notice text={error || categories.error} error />
      {!!error && (
        <Button
          title="Discard edits & reload categories"
          variant="outline"
          busy={busy}
          onPress={() => {
            void run(async () => {
              const fresh = await api<List<Category>>("/categories");
              const latest = fresh.items.find(
                (item) => item.id === editing?.id,
              );
              if (latest) {
                setEditing(latest);
                setName(latest.name);
                setMalayalam(latest.nameMalayalam);
                setKind(latest.kind);
                setPosition(String(latest.position));
              } else reset();
              categories.reload();
            });
          }}
        />
      )}
      <Panel>
        <Heading level={3}>
          {editing ? "Edit category" : "Add category"}
        </Heading>
        <Field label="Name" value={name} onChangeText={setName} />
        <Field
          label="Malayalam name"
          value={malayalam}
          onChangeText={setMalayalam}
        />
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 7 }}>
          {(["purpose", "occasion", "theme"] as const).map((value) => (
            <Pill
              key={value}
              label={value.charAt(0).toUpperCase() + value.slice(1)}
              active={value === kind}
              onPress={() => setKind(value)}
            />
          ))}
        </View>
        <Field
          label="Display order"
          value={position}
          onChangeText={setPosition}
          keyboardType="number-pad"
        />
        <Button
          title={editing ? "Save category" : "Add category"}
          busy={busy}
          disabled={!name.trim()}
          onPress={() => {
            void run(async () => {
              const body = {
                name: name.trim(),
                nameMalayalam: malayalam,
                kind,
                position: Number(position) || 0,
              };
              if (editing)
                await put(`/admin/categories/${editing.id}`, {
                  ...body,
                  version: editing.version,
                });
              else await post("/admin/categories", body);
              reset();
            });
          }}
        />
        {editing && (
          <Button title="Cancel editing" variant="quiet" onPress={reset} />
        )}
      </Panel>
      <View>
        {categories.data?.items.map((category) => (
          <View
            key={category.id}
            style={{
              gap: 12,
              paddingVertical: 16,
              borderBottomWidth: 1,
              borderColor: theme.line,
            }}
          >
            <View style={{ gap: 4 }}>
              <Copy style={{ fontWeight: "600" }}>{category.name}</Copy>
              <Copy muted style={{ fontSize: 14 }}>
                {category.kind.charAt(0).toUpperCase() + category.kind.slice(1)}{" "}
                · Order {category.position}
              </Copy>
            </View>
            <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
              <Button
                title="Edit"
                small
                variant="outline"
                onPress={() => {
                  setEditing(category);
                  setName(category.name);
                  setMalayalam(category.nameMalayalam);
                  setKind(category.kind);
                  setPosition(String(category.position));
                }}
              />
              <Button
                title={removeID === category.id ? "Confirm deletion" : "Delete"}
                small
                variant="danger"
                busy={busy}
                onPress={() => {
                  if (removeID !== category.id) {
                    setRemoveID(category.id);
                    return;
                  }
                  void run(async () => {
                    await api(
                      `/admin/categories/${category.id}?version=${category.version}`,
                      { method: "DELETE" },
                    );
                    setRemoveID("");
                  });
                }}
              />
            </View>
            {removeID === category.id && (
              <Copy muted>
                This removes the category from all hymns. Hymns remain in the
                collection.
              </Copy>
            )}
          </View>
        ))}
      </View>
    </View>
  );
}
export function ConfigManager() {
  const { revision, invalidate } = useApp();
  const config = useResource<AppConfig>("/config", revision);
  const [form, setForm] = useState({
    appName: "Holy Hymns",
    announcement: "",
    aboutText: "",
    supportEmail: "",
    version: 0,
  });
  const [loaded, setLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    if (config.data && !loaded) {
      setForm(config.data);
      setLoaded(true);
    }
  }, [config.data, loaded]);
  if (!loaded)
    return config.error ? <Notice error text={config.error} /> : <Loading />;
  return (
    <View style={{ gap: 20 }}>
      <Heading level={2}>App settings</Heading>
      <Copy muted>Changes appear in the app after saving.</Copy>
      <Notice text={error} error />
      {!!error && (
        <Button
          title="Reload latest settings"
          variant="outline"
          onPress={() => {
            setBusy(true);
            void api<AppConfig>("/config")
              .then((next) => {
                setForm(next);
                setError("");
                setSaved(false);
              })
              .catch((error) => setError(message(error)))
              .finally(() => setBusy(false));
          }}
        />
      )}
      <Notice text={saved ? "App settings saved." : ""} />
      {(
        [
          { key: "appName", label: "App name" },
          { key: "announcement", label: "Announcement" },
          { key: "aboutText", label: "About Holy Hymns" },
          { key: "supportEmail", label: "Support email" },
        ] as const
      ).map((item) => (
        <Field
          key={item.key}
          label={item.label}
          value={form[item.key]}
          multiline={item.key === "aboutText" || item.key === "announcement"}
          onChangeText={(value) => {
            setSaved(false);
            setForm((previous) => ({ ...previous, [item.key]: value }));
          }}
        />
      ))}
      <Button
        title="Save changes"
        busy={busy}
        onPress={() => {
          setBusy(true);
          setError("");
          void put<AppConfig>("/admin/config", {
            appName: form.appName,
            announcement: form.announcement,
            aboutText: form.aboutText,
            supportEmail: form.supportEmail,
            version: form.version,
          })
            .then((next) => {
              setForm(next);
              invalidate();
              setSaved(true);
            })
            .catch((error) => setError(message(error)))
            .finally(() => setBusy(false));
        }}
      />
    </View>
  );
}
export function UserManager() {
  const theme = useTheme();
  const { user } = useApp();
  const [offset, setOffset] = useState(0);
  const users = useResource<List<User>>(
    `/admin/users?limit=30&offset=${offset}`,
  );
  const [email, setEmail] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [ownerConfirmation, setOwnerConfirmation] = useState("");
  const [expandedPerson, setExpandedPerson] = useState("");
  const run = async (task: () => Promise<void>) => {
    setBusy(true);
    setNotice("");
    setError("");
    try {
      await task();
      users.reload();
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <View style={{ gap: 20 }}>
      <Heading level={2}>People & access</Heading>
      <Notice text={error || users.error} error />
      <Notice text={notice} />
      {user?.role === "owner" && (
        <Panel>
          <Heading level={3}>Invite an administrator</Heading>
          <Field
            label="Email address"
            value={email}
            onChangeText={setEmail}
            keyboardType="email-address"
            autoCapitalize="none"
          />
          <Button
            title="Send invitation"
            busy={busy}
            disabled={!email.trim()}
            onPress={() => {
              void run(async () => {
                const result = await post<{ message: string }>(
                  "/admin/invitations",
                  { email },
                );
                setNotice(result.message || "Invitation sent.");
                setEmail("");
              });
            }}
          />
        </Panel>
      )}
      {users.loading ? (
        <Loading />
      ) : (
        <View>
          {users.data?.items.map((person) => (
            <View
              key={person.id}
              style={{
                gap: 12,
                paddingVertical: 16,
                borderBottomWidth: 1,
                borderColor: theme.line,
              }}
            >
              <View
                style={{ flexDirection: "row", alignItems: "center", gap: 12 }}
              >
                <View style={{ flex: 1, gap: 4 }}>
                  <Copy style={{ fontWeight: "600" }}>
                    {person.name || person.email}
                  </Copy>
                  {!!person.name && (
                    <Copy muted style={{ fontSize: 14 }}>
                      {person.email}
                    </Copy>
                  )}
                  <Copy muted style={{ fontSize: 14 }}>
                    {person.role.charAt(0).toUpperCase() + person.role.slice(1)}
                    {person.suspended ? " · Suspended" : ""}
                    {person.id === user?.id ? " · You" : ""}
                  </Copy>
                </View>
                {person.id !== user?.id && (
                  <Button
                    title={expandedPerson === person.id ? "Close" : "Manage"}
                    variant="quiet"
                    small
                    onPress={() => {
                      setExpandedPerson((value) =>
                        value === person.id ? "" : person.id,
                      );
                      setOwnerConfirmation("");
                    }}
                  />
                )}
              </View>
              {person.id !== user?.id && expandedPerson === person.id && (
                <>
                  {(user?.role === "owner" || person.role === "reader") && (
                    <Button
                      title={
                        person.suspended ? "Restore access" : "Suspend account"
                      }
                      variant={person.suspended ? "outline" : "danger"}
                      busy={busy}
                      onPress={() => {
                        void run(async () => {
                          await put(`/admin/users/${person.id}`, {
                            suspended: !person.suspended,
                          });
                        });
                      }}
                    />
                  )}
                  {user?.role === "owner" && (
                    <>
                      <Button
                        title={
                          person.role === "admin"
                            ? "Remove admin access"
                            : person.role === "owner"
                              ? "Change owner to admin"
                              : "Grant admin access"
                        }
                        variant="outline"
                        busy={busy}
                        onPress={() => {
                          void run(async () => {
                            await put(`/admin/users/${person.id}`, {
                              role:
                                person.role === "admin" ? "reader" : "admin",
                            });
                          });
                        }}
                      />
                      {person.role !== "owner" && (
                        <Button
                          title={
                            ownerConfirmation === person.id
                              ? "Confirm full owner access"
                              : "Grant owner access"
                          }
                          variant="quiet"
                          busy={busy}
                          disabled={person.suspended}
                          onPress={() => {
                            if (ownerConfirmation !== person.id) {
                              setOwnerConfirmation(person.id);
                              return;
                            }
                            void run(async () => {
                              await put(`/admin/users/${person.id}`, {
                                role: "owner",
                              });
                              setOwnerConfirmation("");
                              setNotice("Owner access granted.");
                            });
                          }}
                        />
                      )}
                      {ownerConfirmation === person.id && (
                        <Copy muted>
                          An owner can manage all accounts, grant administrator
                          access, and change other owners. Confirm only for
                          someone you trust to run Holy Hymns.
                        </Copy>
                      )}
                    </>
                  )}
                </>
              )}
            </View>
          ))}
        </View>
      )}
      <View style={{ flexDirection: "row", justifyContent: "space-between" }}>
        <Button
          title="Previous"
          variant="outline"
          disabled={offset === 0 || users.loading}
          onPress={() => setOffset(Math.max(0, offset - 30))}
        />
        <Button
          title="Next"
          variant="outline"
          disabled={
            users.loading ||
            (users.data?.items.length || 0) < 30 ||
            offset + 30 >= (users.data?.total ?? Infinity)
          }
          onPress={() => setOffset(offset + 30)}
        />
      </View>
    </View>
  );
}
export function AnalyticsScreen() {
  const theme = useTheme();
  const [days, setDays] = useState(30);
  const report = useResource<Analytics>(`/admin/analytics?days=${days}`);
  return (
    <View style={{ gap: 20 }}>
      <Heading level={2}>Analytics</Heading>
      <Copy muted style={{ fontSize: 14 }}>
        Anonymous, opted-in activity. Staff activity is excluded.
      </Copy>
      <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 7 }}>
        {[7, 30, 90].map((value) => (
          <Pill
            key={value}
            label={`${value} days`}
            active={days === value}
            onPress={() => setDays(value)}
          />
        ))}
      </View>
      {report.loading ? (
        <Loading />
      ) : report.error ? (
        <Notice text={report.error} error />
      ) : (
        report.data && (
          <>
            <View>
              {Object.entries(report.data.totals).map(([key, count]) => (
                <View
                  key={key}
                  style={{
                    flexDirection: "row",
                    alignItems: "center",
                    justifyContent: "space-between",
                    gap: 16,
                    paddingVertical: 16,
                    borderBottomWidth: 1,
                    borderColor: theme.line,
                  }}
                >
                  <Copy style={{ flex: 1 }}>
                    {(
                      {
                        song_open: "Hymn opens",
                        category_open: "Category opens",
                        search: "Searches",
                        no_results: "No-result searches",
                        favorite: "Hymns saved",
                      } as Record<string, string>
                    )[key] || key}
                  </Copy>
                  <Copy style={{ fontSize: 22, fontWeight: "600" }}>
                    {count.toLocaleString()}
                  </Copy>
                </View>
              ))}
            </View>
            <Heading level={3}>Most read hymns</Heading>
            {report.data.topSongs.length ? (
              report.data.topSongs.map((song) => (
                <View
                  key={song.id}
                  style={{
                    flexDirection: "row",
                    justifyContent: "space-between",
                    gap: 20,
                    borderBottomWidth: 1,
                    borderColor: theme.line,
                    paddingVertical: 12,
                  }}
                >
                  <Copy style={{ flex: 1 }}>{song.title}</Copy>
                  <Copy>{song.count}</Copy>
                </View>
              ))
            ) : (
              <Copy muted>No activity yet.</Copy>
            )}
            <Heading level={3}>Daily activity</Heading>
            {report.data.daily.map((day, index) => (
              <View
                key={`${day.date}:${day.event}:${index}`}
                style={{
                  flexDirection: "row",
                  justifyContent: "space-between",
                  gap: 12,
                }}
              >
                <Copy muted style={{ flex: 1, fontSize: 14 }}>
                  {day.date.slice(0, 10)} · {day.event.replaceAll("_", " ")}
                </Copy>
                <Copy style={{ fontSize: 16 }}>{day.count}</Copy>
              </View>
            ))}
            {!!report.data.mailAlerts?.length && (
              <Panel>
                <Heading level={3}>Email delivery alerts</Heading>
                {report.data.mailAlerts.map((alert, index) => (
                  <Copy key={index}>
                    {alert.message} · {alert.occurrences} occurrence
                    {alert.occurrences === 1 ? "" : "s"}
                  </Copy>
                ))}
              </Panel>
            )}
          </>
        )
      )}
    </View>
  );
}
