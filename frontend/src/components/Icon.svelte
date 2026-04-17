<script lang="ts">
    import { icons } from "../lib/icons";

    export let name: string;
    export let size: number = 18;
    export let strokeWidth: number = 2;

    $: path = icons[name] ?? "";
</script>

<svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width={strokeWidth}
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    style="display:inline-block;vertical-align:middle;flex-shrink:0"
>
    {@html path}
</svg>
