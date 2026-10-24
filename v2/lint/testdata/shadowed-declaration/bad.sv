module top #(parameter W = 4) (input clk, input [W-1:0] data);
  logic [W-1:0] state;
  integer i;
  function int g(input int state);     // want
    return state;
  endfunction
  always_ff @(posedge clk) begin : blk
    logic [W-1:0] data;                // want
    for (int i = 0; i < 2; i++) state <= data;  // want
  end
  if (W > 1) begin : gen
    logic [3:0] clk;                   // want
  end
endmodule
