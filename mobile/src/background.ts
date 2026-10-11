// Background check, every ~15 minutes as Android allows: the same pass as the app's own polling.
import * as BackgroundTask from "expo-background-task";
import * as TaskManager from "expo-task-manager";
import { checkAll } from "./poll";

export const TASK = "foray-check";

TaskManager.defineTask(TASK, async () => {
  try { await checkAll({ notify: true }); return BackgroundTask.BackgroundTaskResult.Success; }
  catch { return BackgroundTask.BackgroundTaskResult.Failed; }
});

export async function registerBackground() {
  try {
    const s = await BackgroundTask.getStatusAsync();
    if (s === BackgroundTask.BackgroundTaskStatus.Available && !(await TaskManager.isTaskRegisteredAsync(TASK)))
      await BackgroundTask.registerTaskAsync(TASK, { minimumInterval: 15 });
  } catch { /* background checks are a bonus; the app still checks while open */ }
}
