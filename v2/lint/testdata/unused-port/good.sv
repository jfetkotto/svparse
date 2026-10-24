module top (input clk, input [3:0] data, input _spare, input unused_rst, inout pad, output logic [3:0] q);
  assign pad = data[0];
  always_ff @(posedge clk) q <= data;
endmodule
