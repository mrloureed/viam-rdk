<script setup lang="ts">

import { grpc } from '@improbable-eng/grpc-web';
import { Client, gantryApi } from '@viamrobotics/sdk';
import { displayError } from '../lib/error';
import { rcLogConditionally } from '../lib/log';

const props = defineProps<{
  name: string
  status: {
    parts: {
      pos: number
      axis: number
      length: number
    }[]
  }
  client: Client
}>();

const increment = (axis: number, amount: number) => {
  const pos: number[] = [];
  for (let i = 0; i < props.status.parts.length; i += 1) {
    pos[i] = props.status.parts[i]!.pos;
  }
  pos[axis] += amount;

  const req = new gantryApi.MoveToPositionRequest();
  req.setName(props.name);
  req.setPositionsMmList(pos);

  rcLogConditionally(req);
  props.client.gantryService.moveToPosition(req, new grpc.Metadata(), displayError);
};

const stop = () => {
  const req = new gantryApi.StopRequest();
  req.setName(props.name);

  rcLogConditionally(req);
  props.client.gantryService.stop(req, new grpc.Metadata(), displayError);
};

</script>

<template>
  <v-collapse
    :title="name"
    class="gantry"
  >
    <v-breadcrumbs
      slot="title"
      crumbs="gantry"
    />
    <div
      slot="header"
      class="flex items-center justify-between gap-2"
    >
      <v-button
        variant="danger"
        icon="stop-circle"
        label="STOP"
        @click.stop="stop"
      />
    </div>
    <div class="overflow-auto border border-t-0 border-medium p-4">
      <table class="border border-t-0 border-medium p-4">
        <thead>
          <tr>
            <th class="border border-medium p-2">
              axis
            </th>
            <th
              class="border border-medium p-2"
              colspan="2"
            >
              position
            </th>
            <th class="border border-medium p-2">
              length
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="pp in status.parts"
            :key="pp.axis"
          >
            <th class="border border-medium p-2">
              {{ pp.axis }}
            </th>
            <td class="flex gap-2 p-2">
              <v-button
                class="flex-nowrap"
                label="--"
                @click="increment(pp.axis, -10)"
              />
              <v-button
                label="-"
                @click="increment(pp.axis, -1)"
              />
              <v-button
                label="+"
                @click="increment(pp.axis, 1)"
              />
              <v-button
                label="++"
                @click="increment(pp.axis, 10)"
              />
            </td>
            <td class="border border-medium p-2">
              {{ pp.pos.toFixed(2) }}
            </td>
            <td class="border border-medium p-2">
              {{ pp.length }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </v-collapse>
</template>
